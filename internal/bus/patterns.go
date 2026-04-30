package bus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// RequestResponse handles synchronous agent-to-agent communication.
// It allows an agent to send a request and wait for a response with timeout.
type RequestResponse struct {
	bus    Bus
	logger *slog.Logger
}

// NewRequestResponse creates a new RequestResponse helper.
func NewRequestResponse(bus Bus) *RequestResponse {
	return &RequestResponse{
		bus:    bus,
		logger: slog.Default(),
	}
}

// RequestResponseOption configures a RequestResponse instance.
type RequestResponseOption func(*RequestResponse)

// WithRequestResponseLogger sets the logger.
func WithRequestResponseLogger(logger *slog.Logger) RequestResponseOption {
	return func(rr *RequestResponse) {
		rr.logger = logger
	}
}

// Request sends a message to a specific agent and waits for a response.
// The response topic is derived from the request topic by appending ".response".
// The responder should publish to this topic with ToAgent set to the requester.
func (rr *RequestResponse) Request(ctx context.Context, toAgent, topic string, payload any, timeout time.Duration) (BusMessage, error) {
	requesterID := "requester" // In practice, this would come from the agent context
	responseTopic := topic + ".response"
	correlationID := generateMessageID()

	// Create response channel
	responseCh := make(chan BusMessage, 1)

	// Subscribe to responses
	sub, err := rr.bus.SubscribeWithFilter(
		[]string{responseTopic},
		func(ctx context.Context, msg BusMessage) error {
			select {
			case responseCh <- msg:
			default:
				// Channel full, drop (shouldn't happen with size 1)
			}
			return nil
		},
		func(msg BusMessage) bool {
			// Match by correlation ID
			cid, ok := msg.GetMetadata("correlation_id")
			return ok && cid == correlationID
		},
	)
	if err != nil {
		return BusMessage{}, fmt.Errorf("failed to subscribe for response: %w", err)
	}
	defer sub.Unsubscribe()

	// Create and send request
	reqMsg := NewBusMessage(topic, requesterID, toAgent, payload)
	reqMsg.SetMetadata("correlation_id", correlationID)
	reqMsg.SetMetadata("response_topic", responseTopic)
	reqMsg.SetMetadata("expects_response", true)

	if err := rr.bus.Publish(ctx, topic, reqMsg); err != nil {
		return BusMessage{}, fmt.Errorf("failed to send request: %w", err)
	}

	rr.logger.Debug("Sent request",
		"correlation_id", correlationID,
		"to_agent", toAgent,
		"topic", topic,
		"timeout", timeout,
	)

	// Wait for response
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	select {
	case resp := <-responseCh:
		rr.logger.Debug("Received response",
			"correlation_id", correlationID,
			"from_agent", resp.FromAgent,
		)
		return resp, nil
	case <-ctx.Done():
		return BusMessage{}, &MailboxError{
			Type:    MailboxErrorTimeout,
			Message: fmt.Sprintf("request to %s timed out", toAgent),
			Err:     ctx.Err(),
		}
	}
}

// Respond sends a response to a request. The request message should contain
// the correlation_id and response_topic metadata.
func (rr *RequestResponse) Respond(ctx context.Context, request BusMessage, payload any) error {
	responseTopic, ok := request.GetMetadata("response_topic")
	if !ok {
		return fmt.Errorf("request does not contain response_topic metadata")
	}
	responseTopicStr, ok := responseTopic.(string)
	if !ok {
		return fmt.Errorf("response_topic metadata is not a string")
	}

	correlationID, ok := request.GetMetadata("correlation_id")
	if !ok {
		return fmt.Errorf("request does not contain correlation_id metadata")
	}

	respMsg := NewBusMessage(responseTopicStr, "responder", request.FromAgent, payload)
	respMsg.SetMetadata("correlation_id", correlationID)
	respMsg.SetMetadata("in_response_to", request.ID)
	respMsg.SetMetadata("is_response", true)

	return rr.bus.Publish(ctx, responseTopicStr, respMsg)
}

// BroadcastResult holds the results of a broadcast operation.
type BroadcastResult struct {
	// RequestID is the ID of the original broadcast request.
	RequestID string

	// Responses contains all received responses, keyed by the responding agent's ID.
	Responses map[string]BusMessage

	// Respondents is the list of agent IDs that responded.
	Respondents []string

	// ExpectedCount is the number of agents that were expected to respond.
	ExpectedCount int

	// Elapsed is how long the broadcast took.
	Elapsed time.Duration
}

// ResponseCount returns the number of responses received.
func (r *BroadcastResult) ResponseCount() int {
	return len(r.Responses)
}

// AllResponded returns true if all expected agents responded.
func (r *BroadcastResult) AllResponded() bool {
	return len(r.Responses) >= r.ExpectedCount
}

// GetResponse returns the response from a specific agent, or an error if not found.
func (r *BroadcastResult) GetResponse(agentID string) (BusMessage, error) {
	resp, ok := r.Responses[agentID]
	if !ok {
		return BusMessage{}, fmt.Errorf("no response from agent %s", agentID)
	}
	return resp, nil
}

// RequestBroadcast sends a request to multiple agents and collects their responses.
type RequestBroadcast struct {
	bus    Bus
	logger *slog.Logger
}

// NewRequestBroadcast creates a new RequestBroadcast helper.
func NewRequestBroadcast(bus Bus) *RequestBroadcast {
	return &RequestBroadcast{
		bus:    bus,
		logger: slog.Default(),
	}
}

// RequestBroadcastOption configures a RequestBroadcast instance.
type RequestBroadcastOption func(*RequestBroadcast)

// WithBroadcastLogger sets the logger.
func WithBroadcastLogger(logger *slog.Logger) RequestBroadcastOption {
	return func(rb *RequestBroadcast) {
		rb.logger = logger
	}
}

// Broadcast sends a request to multiple agents and collects responses.
// It waits until all agents respond or the timeout expires.
func (rb *RequestBroadcast) Broadcast(
	ctx context.Context,
	topic string,
	targetAgents []string,
	payload any,
	timeout time.Duration,
) (*BroadcastResult, error) {
	requesterID := "broadcaster"
	responseTopic := topic + ".response"
	correlationID := generateMessageID()

	startTime := time.Now()

	// Setup response collection
	mu := sync.Mutex{}
	responses := make(map[string]BusMessage)
	responseCh := make(chan BusMessage, len(targetAgents))

	// Subscribe to responses
	sub, err := rb.bus.SubscribeWithFilter(
		[]string{responseTopic},
		func(ctx context.Context, msg BusMessage) error {
			select {
			case responseCh <- msg:
			default:
			}
			return nil
		},
		func(msg BusMessage) bool {
			cid, ok := msg.GetMetadata("correlation_id")
			return ok && cid == correlationID
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe for responses: %w", err)
	}
	defer sub.Unsubscribe()

	// Send requests to all target agents
	for _, agentID := range targetAgents {
		reqMsg := NewBusMessage(topic, requesterID, agentID, payload)
		reqMsg.SetMetadata("correlation_id", correlationID)
		reqMsg.SetMetadata("response_topic", responseTopic)
		reqMsg.SetMetadata("expects_response", true)
		reqMsg.SetMetadata("broadcast", true)

		if err := rb.bus.Publish(ctx, topic, reqMsg); err != nil {
			rb.logger.Warn("Failed to send broadcast to agent",
				"agent_id", agentID,
				"error", err,
			)
			continue
		}
	}

	rb.logger.Debug("Sent broadcast",
		"correlation_id", correlationID,
		"target_agents", targetAgents,
		"topic", topic,
		"timeout", timeout,
	)

	// Collect responses
	expectedCount := len(targetAgents)
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case resp := <-responseCh:
			mu.Lock()
			responses[resp.FromAgent] = resp
			count := len(responses)
			mu.Unlock()

			rb.logger.Debug("Received broadcast response",
				"correlation_id", correlationID,
				"from_agent", resp.FromAgent,
				"progress", fmt.Sprintf("%d/%d", count, expectedCount),
			)

			if count >= expectedCount {
				// All responses received
				return rb.buildResult(correlationID, responses, expectedCount, startTime), nil
			}

		case <-timeoutCtx.Done():
			// Timeout expired
			rb.logger.Debug("Broadcast timeout",
				"correlation_id", correlationID,
				"received", len(responses),
				"expected", expectedCount,
			)
			return rb.buildResult(correlationID, responses, expectedCount, startTime), nil
		}
	}
}

// buildResult creates a BroadcastResult from collected responses.
func (rb *RequestBroadcast) buildResult(
	correlationID string,
	responses map[string]BusMessage,
	expectedCount int,
	startTime time.Time,
) *BroadcastResult {
	respondents := make([]string, 0, len(responses))
	for agentID := range responses {
		respondents = append(respondents, agentID)
	}

	return &BroadcastResult{
		RequestID:     correlationID,
		Responses:     responses,
		Respondents:   respondents,
		ExpectedCount: expectedCount,
		Elapsed:       time.Since(startTime),
	}
}

// Vote represents a single vote in a consensus operation.
type Vote struct {
	// VoterID is the ID of the agent casting the vote.
	VoterID string

	// Value is the voted value (e.g., "yes"/"no", an option ID, etc.).
	Value string

	// Weight is the weight of this vote (default 1). Useful for weighted voting.
	Weight float64

	// Reason is an optional explanation for the vote.
	Reason string
}

// ConsensusResult holds the result of a consensus operation.
type ConsensusResult struct {
	// RequestID is the ID of the consensus request.
	RequestID string

	// Votes contains all votes received, keyed by voter ID.
	Votes map[string]Vote

	// Winner is the value that won the consensus.
	Winner string

	// WinnerWeight is the total weight of the winning value.
	WinnerWeight float64

	// TotalWeight is the sum of all vote weights.
	TotalWeight float64

	// QuorumReached is true if quorum was achieved.
	QuorumReached bool

	// Tally contains the weight for each voted value.
	Tally map[string]float64

	// Elapsed is how long the consensus took.
	Elapsed time.Duration
}

// VoteCount returns the number of votes received.
func (r *ConsensusResult) VoteCount() int {
	return len(r.Votes)
}

// Majority returns true if the winner has more than half the weight.
func (r *ConsensusResult) Majority() bool {
	return r.TotalWeight > 0 && r.WinnerWeight > r.TotalWeight/2
}

// Unanimous returns true if all voters chose the same value.
func (r *ConsensusResult) Unanimous() bool {
	return len(r.Tally) == 1
}

// Consensus implements voting-based consensus among agents.
type Consensus struct {
	bus    Bus
	logger *slog.Logger
}

// NewConsensus creates a new Consensus helper.
func NewConsensus(bus Bus) *Consensus {
	return &Consensus{
		bus:    bus,
		logger: slog.Default(),
	}
}

// ConsensusOption configures a Consensus instance.
type ConsensusOption func(*Consensus)

// WithConsensusLogger sets the logger.
func WithConsensusLogger(logger *slog.Logger) ConsensusOption {
	return func(c *Consensus) {
		c.logger = logger
	}
}

// QuorumStrategy defines how quorum is determined.
type QuorumStrategy int

const (
	// QuorumMajority requires more than half the weight to agree.
	QuorumMajority QuorumStrategy = iota

	// QuorumAll requires all agents to vote.
	QuorumAll

	// QuorumSimple requires any response (first value wins if tied).
	QuorumSimple

	// QuorumThreshold requires a minimum total weight threshold.
	QuorumThreshold
)

// ConsensusConfig configures a consensus operation.
type ConsensusConfig struct {
	// Strategy determines how quorum is calculated.
	Strategy QuorumStrategy

	// Threshold is the minimum weight required for QuorumThreshold strategy.
	Threshold float64

	// Timeout is how long to wait for votes.
	Timeout time.Duration
}

// DefaultConsensusConfig returns a default configuration.
func DefaultConsensusConfig() ConsensusConfig {
	return ConsensusConfig{
		Strategy: QuorumMajority,
		Timeout:  30 * time.Second,
	}
}

// Propose starts a consensus round by asking agents to vote on a proposal.
func (c *Consensus) Propose(
	ctx context.Context,
	topic string,
	voters []string,
	proposal any,
	config ConsensusConfig,
) (*ConsensusResult, error) {
	requesterID := "proposer"
	responseTopic := topic + ".vote"
	correlationID := generateMessageID()

	startTime := time.Now()

	// Setup vote collection
	mu := sync.Mutex{}
	votes := make(map[string]Vote)
	voteCh := make(chan BusMessage, len(voters))

	// Subscribe to votes
	sub, err := c.bus.SubscribeWithFilter(
		[]string{responseTopic},
		func(ctx context.Context, msg BusMessage) error {
			select {
			case voteCh <- msg:
			default:
			}
			return nil
		},
		func(msg BusMessage) bool {
			cid, ok := msg.GetMetadata("correlation_id")
			return ok && cid == correlationID
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe for votes: %w", err)
	}
	defer sub.Unsubscribe()

	// Send proposal to all voters
	for _, voterID := range voters {
		proposalMsg := NewBusMessage(topic, requesterID, voterID, proposal)
		proposalMsg.SetMetadata("correlation_id", correlationID)
		proposalMsg.SetMetadata("response_topic", responseTopic)
		proposalMsg.SetMetadata("consensus_request", true)
		proposalMsg.SetMetadata("vote_options", "yes,no") // Default options

		if err := c.bus.Publish(ctx, topic, proposalMsg); err != nil {
			c.logger.Warn("Failed to send proposal to voter",
				"voter_id", voterID,
				"error", err,
			)
		}
	}

	c.logger.Debug("Sent consensus proposal",
		"correlation_id", correlationID,
		"voters", voters,
		"topic", topic,
		"strategy", config.Strategy,
	)

	// Collect votes
	timeoutCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	for {
		select {
		case msg := <-voteCh:
			// Parse vote from message
			vote := c.parseVote(msg)

			mu.Lock()
			votes[vote.VoterID] = vote
			mu.Unlock()

			c.logger.Debug("Received vote",
				"correlation_id", correlationID,
				"voter_id", vote.VoterID,
				"value", vote.Value,
			)

			// Check if quorum is reached
			if c.checkQuorum(votes, voters, config) {
				return c.buildConsensusResult(correlationID, votes, startTime), nil
			}

		case <-timeoutCtx.Done():
			c.logger.Debug("Consensus timeout",
				"correlation_id", correlationID,
				"votes_received", len(votes),
				"voters", len(voters),
			)
			return c.buildConsensusResult(correlationID, votes, startTime), nil
		}
	}
}

// parseVote extracts a Vote from a bus message.
func (c *Consensus) parseVote(msg BusMessage) Vote {
	vote := Vote{
		VoterID: msg.FromAgent,
		Weight:  1.0,
	}

	// Try to extract vote value from payload
	if s, ok := msg.Payload.(string); ok {
		vote.Value = s
	} else if v, ok := msg.GetMetadata("vote_value"); ok {
		vote.Value = fmt.Sprintf("%v", v)
	} else {
		vote.Value = "unknown"
	}

	// Extract optional weight
	if w, ok := msg.GetMetadata("vote_weight"); ok {
		switch v := w.(type) {
		case float64:
			vote.Weight = v
		case int:
			vote.Weight = float64(v)
		}
	}

	// Extract optional reason
	if r, ok := msg.GetMetadata("vote_reason"); ok {
		vote.Reason = fmt.Sprintf("%v", r)
	}

	return vote
}

// checkQuorum determines if quorum has been reached.
func (c *Consensus) checkQuorum(votes map[string]Vote, voters []string, config ConsensusConfig) bool {
	totalWeight := 0.0
	for _, vote := range votes {
		totalWeight += vote.Weight
	}

	// Calculate expected total weight (assuming default weight of 1)
	expectedWeight := float64(len(voters))

	switch config.Strategy {
	case QuorumAll:
		return len(votes) == len(voters)

	case QuorumMajority:
		return totalWeight > expectedWeight/2

	case QuorumSimple:
		return len(votes) > 0

	case QuorumThreshold:
		return totalWeight >= config.Threshold

	default:
		return false
	}
}

// buildConsensusResult creates a ConsensusResult from collected votes.
func (c *Consensus) buildConsensusResult(
	correlationID string,
	votes map[string]Vote,
	startTime time.Time,
) *ConsensusResult {
	// Tally votes
	tally := make(map[string]float64)
	totalWeight := 0.0

	for _, vote := range votes {
		tally[vote.Value] += vote.Weight
		totalWeight += vote.Weight
	}

	// Find winner
	winner := ""
	winnerWeight := 0.0
	for value, weight := range tally {
		if weight > winnerWeight {
			winner = value
			winnerWeight = weight
		}
	}

	return &ConsensusResult{
		RequestID:     correlationID,
		Votes:         votes,
		Winner:        winner,
		WinnerWeight:  winnerWeight,
		TotalWeight:   totalWeight,
		QuorumReached: winnerWeight > 0,
		Tally:         tally,
		Elapsed:       time.Since(startTime),
	}
}

// Vote sends a vote in response to a consensus proposal.
func (c *Consensus) Vote(ctx context.Context, request BusMessage, value string, reason string) error {
	responseTopic, ok := request.GetMetadata("response_topic")
	if !ok {
		return fmt.Errorf("request does not contain response_topic metadata")
	}
	responseTopicStr, ok := responseTopic.(string)
	if !ok {
		return fmt.Errorf("response_topic metadata is not a string")
	}

	correlationID, ok := request.GetMetadata("correlation_id")
	if !ok {
		return fmt.Errorf("request does not contain correlation_id metadata")
	}

	voteMsg := NewBusMessage(responseTopicStr, "voter", request.FromAgent, value)
	voteMsg.SetMetadata("correlation_id", correlationID)
	voteMsg.SetMetadata("vote_value", value)
	voteMsg.SetMetadata("vote_reason", reason)
	voteMsg.SetMetadata("is_vote", true)

	return c.bus.Publish(ctx, responseTopicStr, voteMsg)
}

// Bid represents an offer in an auction.
type Bid struct {
	// BidderID is the ID of the agent making the bid.
	BidderID string

	// Value is the bid value (e.g., a price, score, confidence level).
	Value float64

	// Proposal is an optional description of what the bidder is offering.
	Proposal string

	// Metadata contains additional bid details.
	Metadata map[string]any
}

// AuctionResult holds the result of an auction operation.
type AuctionResult struct {
	// RequestID is the ID of the auction request.
	RequestID string

	// Bids contains all bids received, keyed by bidder ID.
	Bids map[string]Bid

	// Winner is the winning bid (if any).
	Winner *Bid

	// WinnerID is the ID of the winning bidder.
	WinnerID string

	// BidCount is the number of bids received.
	BidCount int

	// Elapsed is how long the auction took.
	Elapsed time.Duration
}

// HasWinner returns true if the auction produced a winner.
func (r *AuctionResult) HasWinner() bool {
	return r.Winner != nil
}

// GetBid returns the bid from a specific agent, or an error if not found.
func (r *AuctionResult) GetBid(bidderID string) (Bid, error) {
	bid, ok := r.Bids[bidderID]
	if !ok {
		return Bid{}, fmt.Errorf("no bid from agent %s", bidderID)
	}
	return bid, nil
}

// AuctionStrategy defines how the winning bid is selected.
type AuctionStrategy int

const (
	// AuctionHighestBid selects the highest bid value.
	AuctionHighestBid AuctionStrategy = iota

	// AuctionLowestBid selects the lowest bid value.
	AuctionLowestBid

	// AuctionFirstBid selects the first bid received.
	AuctionFirstBid
)

// AuctionConfig configures an auction operation.
type AuctionConfig struct {
	// Strategy determines how the winner is selected.
	Strategy AuctionStrategy

	// MinBids is the minimum number of bids required to select a winner.
	// Set to 0 to accept any number of bids.
	MinBids int

	// Timeout is how long to wait for bids.
	Timeout time.Duration
}

// DefaultAuctionConfig returns a default configuration.
func DefaultAuctionConfig() AuctionConfig {
	return AuctionConfig{
		Strategy: AuctionHighestBid,
		MinBids:  1,
		Timeout:  30 * time.Second,
	}
}

// Auction implements competitive bidding among agents.
type Auction struct {
	bus    Bus
	logger *slog.Logger
}

// NewAuction creates a new Auction helper.
func NewAuction(bus Bus) *Auction {
	return &Auction{
		bus:    bus,
		logger: slog.Default(),
	}
}

// AuctionOption configures an Auction instance.
type AuctionOption func(*Auction)

// WithAuctionLogger sets the logger.
func WithAuctionLogger(logger *slog.Logger) AuctionOption {
	return func(a *Auction) {
		a.logger = logger
	}
}

// Start begins an auction by inviting agents to bid.
func (a *Auction) Start(
	ctx context.Context,
	topic string,
	bidders []string,
	item any,
	config AuctionConfig,
) (*AuctionResult, error) {
	requesterID := "auctioneer"
	responseTopic := topic + ".bid"
	correlationID := generateMessageID()

	startTime := time.Now()

	// Setup bid collection
	mu := sync.Mutex{}
	bids := make(map[string]Bid)
	bidOrder := make([]string, 0, len(bidders))
	bidCh := make(chan BusMessage, len(bidders))

	// Subscribe to bids
	sub, err := a.bus.SubscribeWithFilter(
		[]string{responseTopic},
		func(ctx context.Context, msg BusMessage) error {
			select {
			case bidCh <- msg:
			default:
			}
			return nil
		},
		func(msg BusMessage) bool {
			cid, ok := msg.GetMetadata("correlation_id")
			return ok && cid == correlationID
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe for bids: %w", err)
	}
	defer sub.Unsubscribe()

	// Send auction invitation to all bidders
	for _, bidderID := range bidders {
		auctionMsg := NewBusMessage(topic, requesterID, bidderID, item)
		auctionMsg.SetMetadata("correlation_id", correlationID)
		auctionMsg.SetMetadata("response_topic", responseTopic)
		auctionMsg.SetMetadata("auction_request", true)
		auctionMsg.SetMetadata("auction_strategy", config.Strategy)

		if err := a.bus.Publish(ctx, topic, auctionMsg); err != nil {
			a.logger.Warn("Failed to send auction invitation to bidder",
				"bidder_id", bidderID,
				"error", err,
			)
		}
	}

	a.logger.Debug("Started auction",
		"correlation_id", correlationID,
		"bidders", bidders,
		"topic", topic,
		"strategy", config.Strategy,
	)

	// Collect bids
	timeoutCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	for {
		select {
		case msg := <-bidCh:
			// Parse bid from message
			bid := a.parseBid(msg)

			mu.Lock()
			if _, exists := bids[bid.BidderID]; !exists {
				// Only record first bid from each bidder
				bids[bid.BidderID] = bid
				bidOrder = append(bidOrder, bid.BidderID)
			}
			currentBidCount := len(bids)
			mu.Unlock()

			a.logger.Debug("Received bid",
				"correlation_id", correlationID,
				"bidder_id", bid.BidderID,
				"value", bid.Value,
			)

			// Check if we have minimum bids
			if config.MinBids > 0 && currentBidCount >= config.MinBids {
				// For first-bid strategy, return immediately
				if config.Strategy == AuctionFirstBid {
					return a.buildAuctionResult(correlationID, bids, bidOrder, startTime, config.Strategy), nil
				}
			}

		case <-timeoutCtx.Done():
			a.logger.Debug("Auction timeout",
				"correlation_id", correlationID,
				"bids_received", len(bids),
				"bidders", len(bidders),
			)
			return a.buildAuctionResult(correlationID, bids, bidOrder, startTime, config.Strategy), nil
		}
	}
}

// parseBid extracts a Bid from a bus message.
func (a *Auction) parseBid(msg BusMessage) Bid {
	bid := Bid{
		BidderID: msg.FromAgent,
		Metadata: make(map[string]any),
	}

	// Try to extract bid value from payload
	switch v := msg.Payload.(type) {
	case float64:
		bid.Value = v
	case int:
		bid.Value = float64(v)
	case string:
		// Try to parse as float
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			bid.Value = f
		}
		bid.Proposal = v
	}

	// Override with explicit metadata if provided
	if v, ok := msg.GetMetadata("bid_value"); ok {
		switch val := v.(type) {
		case float64:
			bid.Value = val
		case int:
			bid.Value = float64(val)
		}
	}

	if p, ok := msg.GetMetadata("bid_proposal"); ok {
		bid.Proposal = fmt.Sprintf("%v", p)
	}

	// Copy remaining metadata
	for k, v := range msg.Metadata {
		switch k {
		case "correlation_id", "bid_value", "bid_proposal", "is_bid":
			// Skip internal metadata
		default:
			bid.Metadata[k] = v
		}
	}

	return bid
}

// buildAuctionResult creates an AuctionResult from collected bids.
func (a *Auction) buildAuctionResult(
	correlationID string,
	bids map[string]Bid,
	bidOrder []string,
	startTime time.Time,
	strategy AuctionStrategy,
) *AuctionResult {
	result := &AuctionResult{
		RequestID: correlationID,
		Bids:      bids,
		BidCount:  len(bids),
		Elapsed:   time.Since(startTime),
	}

	if len(bids) == 0 {
		return result
	}

	// Find winner based on strategy

	var winnerID string
	var winnerBid *Bid

	switch strategy {
	case AuctionHighestBid:
		for _, bid := range bids {
			if winnerBid == nil || bid.Value > winnerBid.Value {
				winnerBid = &bid
				winnerID = bid.BidderID
			}
		}

	case AuctionLowestBid:
		for _, bid := range bids {
			if winnerBid == nil || bid.Value < winnerBid.Value {
				winnerBid = &bid
				winnerID = bid.BidderID
			}
		}

	case AuctionFirstBid:
		if len(bidOrder) > 0 {
			winnerID = bidOrder[0]
			bid := bids[winnerID]
			winnerBid = &bid
		}
	}

	if winnerBid != nil {
		result.Winner = winnerBid
		result.WinnerID = winnerID
	}

	return result
}

// Bid sends a bid in response to an auction invitation.
func (a *Auction) Bid(ctx context.Context, request BusMessage, value float64, proposal string) error {
	responseTopic, ok := request.GetMetadata("response_topic")
	if !ok {
		return fmt.Errorf("request does not contain response_topic metadata")
	}
	responseTopicStr, ok := responseTopic.(string)
	if !ok {
		return fmt.Errorf("response_topic metadata is not a string")
	}

	correlationID, ok := request.GetMetadata("correlation_id")
	if !ok {
		return fmt.Errorf("request does not contain correlation_id metadata")
	}

	bidMsg := NewBusMessage(responseTopicStr, "bidder", request.FromAgent, value)
	bidMsg.SetMetadata("correlation_id", correlationID)
	bidMsg.SetMetadata("bid_value", value)
	bidMsg.SetMetadata("bid_proposal", proposal)
	bidMsg.SetMetadata("is_bid", true)

	return a.bus.Publish(ctx, responseTopicStr, bidMsg)
}

// Multicast sends a message to a specific set of agents (as opposed to broadcast
// which sends to all subscribers). This is useful when you know exactly which
// agents should receive the message.
type Multicast struct {
	bus    Bus
	logger *slog.Logger
}

// NewMulticast creates a new Multicast helper.
func NewMulticast(bus Bus) *Multicast {
	return &Multicast{
		bus:    bus,
		logger: slog.Default(),
	}
}

// MulticastOption configures a Multicast instance.
type MulticastOption func(*Multicast)

// WithMulticastLogger sets the logger.
func WithMulticastLogger(logger *slog.Logger) MulticastOption {
	return func(m *Multicast) {
		m.logger = logger
	}
}

// MulticastResult holds the result of a multicast operation.
type MulticastResult struct {
	// MessageID is the ID used for the multicast messages.
	MessageID string

	// SentCount is the number of messages successfully sent.
	SentCount int64

	// FailedCount is the number of messages that failed to send.
	FailedCount int64

	// Errors contains any send errors, keyed by agent ID.
	Errors map[string]error

	// TargetAgents is the list of agents that were targeted.
	TargetAgents []string
}

// Send sends a message to multiple specific agents.
// Each agent receives a separate message with ToAgent set to their ID.
func (m *Multicast) Send(
	ctx context.Context,
	topic string,
	targetAgents []string,
	payload any,
) (*MulticastResult, error) {
	senderID := "multicaster"
	messageID := generateMessageID()

	var sentCount int64
	var failedCount int64
	errors := make(map[string]error)
	mu := sync.Mutex{}

	// Send to all targets
	var wg sync.WaitGroup
	for _, agentID := range targetAgents {
		wg.Add(1)
		go func(targetID string) {
			defer wg.Done()

			msg := NewBusMessage(topic, senderID, targetID, payload)
			msg.ID = messageID // Use same ID for all messages in this multicast
			msg.SetMetadata("multicast", true)
			msg.SetMetadata("multicast_id", messageID)

			if err := m.bus.Publish(ctx, topic, msg); err != nil {
				mu.Lock()
				errors[targetID] = err
				failedCount++
				mu.Unlock()

				m.logger.Warn("Failed to multicast to agent",
					"agent_id", targetID,
					"message_id", messageID,
					"error", err,
				)
			} else {
				atomic.AddInt64(&sentCount, 1)
			}
		}(agentID)
	}

	wg.Wait()

	m.logger.Debug("Multicast complete",
		"message_id", messageID,
		"sent", sentCount,
		"failed", failedCount,
		"targets", len(targetAgents),
	)

	return &MulticastResult{
		MessageID:    messageID,
		SentCount:    sentCount,
		FailedCount:  failedCount,
		Errors:       errors,
		TargetAgents: targetAgents,
	}, nil
}
