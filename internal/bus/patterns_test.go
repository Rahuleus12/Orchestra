package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRequestResponse(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)
	if rr == nil {
		t.Fatal("expected RequestResponse to be created")
	}
}

func TestRequestResponse_Request_Success(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)

	// Setup responder
	ready := make(chan struct{})
	go func() {
		// Subscribe to requests
		sub, _ := bus.Subscribe([]string{"request.topic"}, func(ctx context.Context, msg BusMessage) error {
			// Respond after a short delay
			time.Sleep(10 * time.Millisecond)
			return rr.Respond(ctx, msg, "response payload")
		})
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready

		// Keep subscription alive
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	// Make request
	resp, err := rr.Request(context.Background(), "responder-agent", "request.topic", "request payload", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.Payload != "response payload" {
		t.Errorf("expected payload 'response payload', got %v", resp.Payload)
	}

	// Verify response metadata
	isResponse, _ := resp.GetMetadata("is_response")
	if isResponse != true {
		t.Error("expected is_response metadata to be true")
	}
}

func TestRequestResponse_Request_Timeout(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)

	// No responder set up, so request should timeout
	_, err := rr.Request(context.Background(), "nonexistent", "topic", "payload", 50*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}

	var mboxErr *MailboxError
	if !isMailboxError(err, mboxErr, MailboxErrorTimeout) {
		t.Errorf("expected MailboxErrorTimeout, got %v", err)
	}
}

func TestRequestResponse_Request_ContextCancelled(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := rr.Request(ctx, "agent", "topic", "payload", 5*time.Second)
	if err == nil {
		t.Error("expected error when context is cancelled")
	}
}

func TestRequestResponse_Request_CorrelationID(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)

	var receivedCorrelationID string
	var mu sync.Mutex

	// Responder captures correlation ID
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.Subscribe([]string{"topic"}, func(ctx context.Context, msg BusMessage) error {
			mu.Lock()
			if cid, ok := msg.GetMetadata("correlation_id"); ok {
				receivedCorrelationID, _ = cid.(string)
			}
			mu.Unlock()
			return rr.Respond(ctx, msg, "ok")
		})
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	_, err := rr.Request(context.Background(), "agent", "topic", "payload", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	mu.Lock()
	if receivedCorrelationID == "" {
		t.Error("expected correlation ID to be set")
	}
	mu.Unlock()
}

func TestRequestResponse_Respond_MissingResponseTopic(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)

	// Create a request without response_topic metadata
	req := NewBusMessage("topic", "sender", "responder", "payload")

	err := rr.Respond(context.Background(), req, "response")
	if err == nil {
		t.Error("expected error when response_topic is missing")
	}
}

func TestRequestResponse_Respond_MissingCorrelationID(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)

	// Create a request with response_topic but no correlation_id
	req := NewBusMessage("topic", "sender", "responder", "payload")
	req.SetMetadata("response_topic", "topic.response")

	err := rr.Respond(context.Background(), req, "response")
	if err == nil {
		t.Error("expected error when correlation_id is missing")
	}
}

func TestRequestResponse_MultipleRequests(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rr := NewRequestResponse(bus)

	// Track requests received
	var requestCount atomic.Int64
	var responseWg sync.WaitGroup

	// Responder handles multiple requests
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.Subscribe([]string{"topic"}, func(ctx context.Context, msg BusMessage) error {
			requestCount.Add(1)
			responseWg.Add(1)
			go func() {
				defer responseWg.Done()
				time.Sleep(10 * time.Millisecond)
				// Use context.Background() instead of handler context to avoid
				// the response being cancelled when the handler returns
				_ = rr.Respond(context.Background(), msg, "response")
			}()
			return nil
		})
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(1 * time.Second)
	}()

	<-ready // Wait for subscription to be ready

	// Send multiple requests concurrently
	var wg sync.WaitGroup
	numRequests := 5
	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := rr.Request(context.Background(), "agent", "topic", idx, 200*time.Millisecond)
			if err != nil {
				t.Errorf("request %d failed: %v", idx, err)
			}
		}(i)
	}

	// Wait for responses first, then for requests to complete
	responseWg.Wait()
	wg.Wait()

	if requestCount.Load() != int64(numRequests) {
		t.Errorf("expected %d requests received, got %d", numRequests, requestCount.Load())
	}
}

func TestNewRequestBroadcast(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rb := NewRequestBroadcast(bus)
	if rb == nil {
		t.Fatal("expected RequestBroadcast to be created")
	}
}

func TestRequestBroadcast_AllRespond(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rb := NewRequestBroadcast(bus)

	targetAgents := []string{"agent-1", "agent-2", "agent-3"}

	// Setup responders
	var wg sync.WaitGroup
	ready := make(chan struct{}, len(targetAgents))
	for _, agentID := range targetAgents {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sub, _ := bus.SubscribeWithFilter(
				[]string{"broadcast.topic"},
				func(ctx context.Context, msg BusMessage) error {
					responseTopic, _ := msg.GetMetadata("response_topic")
					correlationID, _ := msg.GetMetadata("correlation_id")

					resp := NewBusMessage(responseTopic.(string), id, "broadcaster", "response from "+id)
					resp.SetMetadata("correlation_id", correlationID)
					return bus.Publish(ctx, responseTopic.(string), resp)
				},
				func(msg BusMessage) bool {
					return msg.ToAgent == id
				},
			)
			defer sub.Unsubscribe()
			ready <- struct{}{} // Signal subscription is ready
			time.Sleep(500 * time.Millisecond)
		}(agentID)
	}
	// Wait for all subscriptions to be ready
	for i := 0; i < len(targetAgents); i++ {
		<-ready
	}

	result, err := rb.Broadcast(context.Background(), "broadcast.topic", targetAgents, "question", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	if result.ResponseCount() != 3 {
		t.Errorf("expected 3 responses, got %d", result.ResponseCount())
	}
	if !result.AllResponded() {
		t.Error("expected AllResponded to be true")
	}
	if result.ExpectedCount != 3 {
		t.Errorf("expected ExpectedCount 3, got %d", result.ExpectedCount)
	}

	wg.Wait()
}

func TestRequestBroadcast_PartialResponses(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rb := NewRequestBroadcast(bus)

	targetAgents := []string{"agent-1", "agent-2", "agent-3"}

	// Only agent-1 responds
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.SubscribeWithFilter(
			[]string{"broadcast.topic"},
			func(ctx context.Context, msg BusMessage) error {
				responseTopic, _ := msg.GetMetadata("response_topic")
				correlationID, _ := msg.GetMetadata("correlation_id")

				resp := NewBusMessage(responseTopic.(string), "agent-1", "broadcaster", "response")
				resp.SetMetadata("correlation_id", correlationID)
				return bus.Publish(ctx, responseTopic.(string), resp)
			},
			func(msg BusMessage) bool {
				return msg.ToAgent == "agent-1"
			},
		)
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := rb.Broadcast(context.Background(), "broadcast.topic", targetAgents, "question", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Broadcast failed: %v", err)
	}

	if result.ResponseCount() != 1 {
		t.Errorf("expected 1 response, got %d", result.ResponseCount())
	}
	if result.AllResponded() {
		t.Error("expected AllResponded to be false")
	}
}

func TestRequestBroadcast_Timeout(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	rb := NewRequestBroadcast(bus)

	// No responders
	result, err := rb.Broadcast(context.Background(), "topic", []string{"agent-1"}, "question", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Broadcast should not error on timeout: %v", err)
	}

	if result.ResponseCount() != 0 {
		t.Errorf("expected 0 responses, got %d", result.ResponseCount())
	}
	if result.Elapsed < 40*time.Millisecond {
		t.Errorf("expected elapsed to be close to timeout, got %v", result.Elapsed)
	}
}

func TestBroadcastResult_GetResponse(t *testing.T) {
	result := &BroadcastResult{
		Responses: map[string]BusMessage{
			"agent-1": {Payload: "response 1"},
			"agent-2": {Payload: "response 2"},
		},
	}

	resp, err := result.GetResponse("agent-1")
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if resp.Payload != "response 1" {
		t.Errorf("expected 'response 1', got %v", resp.Payload)
	}

	_, err = result.GetResponse("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

func TestNewConsensus(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	c := NewConsensus(bus)
	if c == nil {
		t.Fatal("expected Consensus to be created")
	}
}

func TestDefaultConsensusConfig(t *testing.T) {
	config := DefaultConsensusConfig()

	if config.Strategy != QuorumMajority {
		t.Errorf("expected QuorumMajority, got %d", config.Strategy)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", config.Timeout)
	}
}

func TestConsensus_Unanimous(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	c := NewConsensus(bus)

	voters := []string{"voter-1", "voter-2", "voter-3"}

	// All voters vote "yes"
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.Subscribe([]string{"proposal.topic"}, func(ctx context.Context, msg BusMessage) error {
			responseTopic, _ := msg.GetMetadata("response_topic")
			correlationID, _ := msg.GetMetadata("correlation_id")

			voteMsg := NewBusMessage(responseTopic.(string), msg.ToAgent, msg.FromAgent, "yes")
			voteMsg.SetMetadata("correlation_id", correlationID)
			voteMsg.SetMetadata("vote_value", "yes")
			return bus.Publish(ctx, responseTopic.(string), voteMsg)
		})
		defer sub.Unsubscribe()
		close(ready) // Signal that subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := c.Propose(context.Background(), "proposal.topic", voters, "should we proceed?", ConsensusConfig{
		Strategy: QuorumAll,
		Timeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	if result.Winner != "yes" {
		t.Errorf("expected winner 'yes', got '%s'", result.Winner)
	}
	if !result.Unanimous() {
		t.Error("expected Unanimous to be true")
	}
	if result.VoteCount() != 3 {
		t.Errorf("expected 3 votes, got %d", result.VoteCount())
	}
}

func TestConsensus_Majority(t *testing.T) {
	bus := NewInMemoryBus()

	c := NewConsensus(bus)

	voters := []string{"voter-1", "voter-2", "voter-3"}

	// voter-1 and voter-2 vote "yes", voter-3 votes "no"
	voteMap := map[string]string{
		"voter-1": "yes",
		"voter-2": "yes",
		"voter-3": "no",
	}

	// Each voter subscribes with a filter for their own ID
	ready := make(chan struct{}, len(voters))
	for _, voterID := range voters {
		go func(id string) {
			sub, _ := bus.SubscribeWithFilter(
				[]string{"proposal.topic"},
				func(ctx context.Context, msg BusMessage) error {
					responseTopic, _ := msg.GetMetadata("response_topic")
					correlationID, _ := msg.GetMetadata("correlation_id")
					vote := voteMap[id]

					voteMsg := NewBusMessage(responseTopic.(string), id, msg.FromAgent, vote)
					voteMsg.SetMetadata("correlation_id", correlationID)
					voteMsg.SetMetadata("vote_value", vote)
					return bus.Publish(ctx, responseTopic.(string), voteMsg)
				},
				func(msg BusMessage) bool {
					return msg.ToAgent == id
				},
			)
			defer sub.Unsubscribe()
			ready <- struct{}{} // Signal this subscription is ready
			time.Sleep(500 * time.Millisecond)
		}(voterID)
	}
	// Wait for all subscriptions to be ready
	for i := 0; i < len(voters); i++ {
		<-ready
	}

	// Use QuorumAll to ensure all votes are counted before returning
	// This allows us to verify the majority calculation is correct
	result, err := c.Propose(context.Background(), "proposal.topic", voters, "proposal", ConsensusConfig{
		Strategy: QuorumAll,
		Timeout:  500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	if result.Winner != "yes" {
		t.Errorf("expected winner 'yes', got '%s'", result.Winner)
	}
	if !result.Majority() {
		t.Error("expected Majority to be true")
	}
	if result.Unanimous() {
		t.Error("expected Unanimous to be false")
	}

	// Check tally
	if result.Tally["yes"] != 2.0 {
		t.Errorf("expected yes tally 2.0, got %v", result.Tally["yes"])
	}
	if result.Tally["no"] != 1.0 {
		t.Errorf("expected no tally 1.0, got %v", result.Tally["no"])
	}
}

func TestConsensus_WeightedVoting(t *testing.T) {
	bus := NewInMemoryBus()

	c := NewConsensus(bus)

	voters := []string{"voter-1", "voter-2", "voter-3"}

	// voter-1 has weight 5, others have weight 1
	// voter-1 votes "no", others vote "yes"
	// "no" should win due to higher weight
	voteMap := map[string]struct {
		value  string
		weight float64
	}{
		"voter-1": {"no", 5.0},
		"voter-2": {"yes", 1.0},
		"voter-3": {"yes", 1.0},
	}

	// Each voter subscribes with a filter for their own ID
	ready := make(chan struct{}, len(voters))
	for _, voterID := range voters {
		go func(id string) {
			sub, _ := bus.SubscribeWithFilter(
				[]string{"proposal.topic"},
				func(ctx context.Context, msg BusMessage) error {
					responseTopic, _ := msg.GetMetadata("response_topic")
					correlationID, _ := msg.GetMetadata("correlation_id")
					voteInfo := voteMap[id]

					voteMsg := NewBusMessage(responseTopic.(string), id, msg.FromAgent, voteInfo.value)
					voteMsg.SetMetadata("correlation_id", correlationID)
					voteMsg.SetMetadata("vote_value", voteInfo.value)
					voteMsg.SetMetadata("vote_weight", voteInfo.weight)
					return bus.Publish(ctx, responseTopic.(string), voteMsg)
				},
				func(msg BusMessage) bool {
					return msg.ToAgent == id
				},
			)
			defer sub.Unsubscribe()
			ready <- struct{}{} // Signal this subscription is ready
			time.Sleep(500 * time.Millisecond)
		}(voterID)
	}
	// Wait for all subscriptions to be ready
	for i := 0; i < len(voters); i++ {
		<-ready
	}

	// Use QuorumAll to ensure all votes are counted before returning
	// This allows us to verify the weighted voting calculation is correct
	result, err := c.Propose(context.Background(), "proposal.topic", voters, "proposal", ConsensusConfig{
		Strategy: QuorumAll,
		Timeout:  500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	if result.Winner != "no" {
		t.Errorf("expected winner 'no' (higher weight), got '%s'", result.Winner)
	}
	if result.WinnerWeight != 5.0 {
		t.Errorf("expected winner weight 5.0, got %v", result.WinnerWeight)
	}
}

func TestConsensus_VoteWithReason(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	c := NewConsensus(bus)

	// Test the Vote helper method
	request := NewBusMessage("topic", "proposer", "voter", "proposal")
	request.SetMetadata("response_topic", "topic.vote")
	request.SetMetadata("correlation_id", "test-id")

	err := c.Vote(context.Background(), request, "yes", "because it's good")
	if err != nil {
		t.Fatalf("Vote failed: %v", err)
	}

	// Verify the vote was published
	sub, _ := bus.Subscribe([]string{"topic.vote"}, func(ctx context.Context, msg BusMessage) error {
		value, _ := msg.GetMetadata("vote_value")
		reason, _ := msg.GetMetadata("vote_reason")

		if value != "yes" {
			t.Errorf("expected vote value 'yes', got %v", value)
		}
		if reason != "because it's good" {
			t.Errorf("expected reason 'because it's good', got %v", reason)
		}
		return nil
	})
	defer sub.Unsubscribe()

	time.Sleep(50 * time.Millisecond)
}

func TestConsensus_Vote_MissingMetadata(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	c := NewConsensus(bus)

	// Missing response_topic
	request := NewBusMessage("topic", "proposer", "voter", "proposal")
	err := c.Vote(context.Background(), request, "yes", "")
	if err == nil {
		t.Error("expected error when response_topic is missing")
	}

	// Missing correlation_id
	request.SetMetadata("response_topic", "topic.vote")
	err = c.Vote(context.Background(), request, "yes", "")
	if err == nil {
		t.Error("expected error when correlation_id is missing")
	}
}

func TestConsensus_QuorumSimple(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	c := NewConsensus(bus)

	voters := []string{"voter-1", "voter-2", "voter-3"}

	// Only voter-1 votes, but QuorumSimple should accept any response
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.SubscribeWithFilter(
			[]string{"proposal.topic"},
			func(ctx context.Context, msg BusMessage) error {
				responseTopic, _ := msg.GetMetadata("response_topic")
				correlationID, _ := msg.GetMetadata("correlation_id")

				voteMsg := NewBusMessage(responseTopic.(string), "voter-1", msg.FromAgent, "yes")
				voteMsg.SetMetadata("correlation_id", correlationID)
				voteMsg.SetMetadata("vote_value", "yes")
				return bus.Publish(ctx, responseTopic.(string), voteMsg)
			},
			func(msg BusMessage) bool {
				return msg.ToAgent == "voter-1"
			},
		)
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := c.Propose(context.Background(), "proposal.topic", voters, "proposal", ConsensusConfig{
		Strategy: QuorumSimple,
		Timeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	if result.Winner != "yes" {
		t.Errorf("expected winner 'yes', got '%s'", result.Winner)
	}
	if !result.QuorumReached {
		t.Error("expected QuorumReached to be true")
	}
}

func TestConsensus_NoVotes(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	c := NewConsensus(bus)

	result, err := c.Propose(context.Background(), "topic", []string{"voter-1"}, "proposal", ConsensusConfig{
		Strategy: QuorumMajority,
		Timeout:  50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Propose should not error: %v", err)
	}

	if result.VoteCount() != 0 {
		t.Errorf("expected 0 votes, got %d", result.VoteCount())
	}
	if result.Winner != "" {
		t.Errorf("expected empty winner, got '%s'", result.Winner)
	}
	if result.QuorumReached {
		t.Error("expected QuorumReached to be false")
	}
}

func TestNewAuction(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)
	if a == nil {
		t.Fatal("expected Auction to be created")
	}
}

func TestDefaultAuctionConfig(t *testing.T) {
	config := DefaultAuctionConfig()

	if config.Strategy != AuctionHighestBid {
		t.Errorf("expected AuctionHighestBid, got %d", config.Strategy)
	}
	if config.MinBids != 1 {
		t.Errorf("expected MinBids 1, got %d", config.MinBids)
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", config.Timeout)
	}
}

func TestAuction_HighestBidWins(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	bidders := []string{"bidder-1", "bidder-2", "bidder-3"}

	// Each bidder bids different amounts
	bidMap := map[string]float64{
		"bidder-1": 50.0,
		"bidder-2": 100.0,
		"bidder-3": 75.0,
	}

	ready := make(chan struct{})
	go func() {
		sub, _ := bus.Subscribe([]string{"auction.topic"}, func(ctx context.Context, msg BusMessage) error {
			responseTopic, _ := msg.GetMetadata("response_topic")
			correlationID, _ := msg.GetMetadata("correlation_id")
			bid := bidMap[msg.ToAgent]

			bidMsg := NewBusMessage(responseTopic.(string), msg.ToAgent, msg.FromAgent, bid)
			bidMsg.SetMetadata("correlation_id", correlationID)
			bidMsg.SetMetadata("bid_value", bid)
			return bus.Publish(ctx, responseTopic.(string), bidMsg)
		})
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := a.Start(context.Background(), "auction.topic", bidders, "item for sale", AuctionConfig{
		Strategy: AuctionHighestBid,
		MinBids:  3,
		Timeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !result.HasWinner() {
		t.Fatal("expected HasWinner to be true")
	}
	if result.WinnerID != "bidder-2" {
		t.Errorf("expected winner 'bidder-2', got '%s'", result.WinnerID)
	}
	if result.Winner.Value != 100.0 {
		t.Errorf("expected winning bid 100.0, got %v", result.Winner.Value)
	}
	if result.BidCount != 3 {
		t.Errorf("expected 3 bids, got %d", result.BidCount)
	}
}

func TestAuction_LowestBidWins(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	bidders := []string{"bidder-1", "bidder-2", "bidder-3"}

	bidMap := map[string]float64{
		"bidder-1": 50.0,
		"bidder-2": 100.0,
		"bidder-3": 75.0,
	}

	ready := make(chan struct{})
	go func() {
		sub, _ := bus.Subscribe([]string{"auction.topic"}, func(ctx context.Context, msg BusMessage) error {
			responseTopic, _ := msg.GetMetadata("response_topic")
			correlationID, _ := msg.GetMetadata("correlation_id")
			bid := bidMap[msg.ToAgent]

			bidMsg := NewBusMessage(responseTopic.(string), msg.ToAgent, msg.FromAgent, bid)
			bidMsg.SetMetadata("correlation_id", correlationID)
			bidMsg.SetMetadata("bid_value", bid)
			return bus.Publish(ctx, responseTopic.(string), bidMsg)
		})
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := a.Start(context.Background(), "auction.topic", bidders, "job contract", AuctionConfig{
		Strategy: AuctionLowestBid,
		MinBids:  3,
		Timeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !result.HasWinner() {
		t.Fatal("expected HasWinner to be true")
	}
	if result.WinnerID != "bidder-1" {
		t.Errorf("expected winner 'bidder-1', got '%s'", result.WinnerID)
	}
	if result.Winner.Value != 50.0 {
		t.Errorf("expected winning bid 50.0, got %v", result.Winner.Value)
	}
}

func TestAuction_FirstBidWins(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	bidders := []string{"bidder-1", "bidder-2"}

	// Only first bidder responds
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.SubscribeWithFilter(
			[]string{"auction.topic"},
			func(ctx context.Context, msg BusMessage) error {
				responseTopic, _ := msg.GetMetadata("response_topic")
				correlationID, _ := msg.GetMetadata("correlation_id")

				bidMsg := NewBusMessage(responseTopic.(string), "bidder-1", msg.FromAgent, 100.0)
				bidMsg.SetMetadata("correlation_id", correlationID)
				bidMsg.SetMetadata("bid_value", 100.0)
				return bus.Publish(ctx, responseTopic.(string), bidMsg)
			},
			func(msg BusMessage) bool {
				return msg.ToAgent == "bidder-1"
			},
		)
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := a.Start(context.Background(), "auction.topic", bidders, "item", AuctionConfig{
		Strategy: AuctionFirstBid,
		MinBids:  1,
		Timeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !result.HasWinner() {
		t.Fatal("expected HasWinner to be true")
	}
	if result.WinnerID != "bidder-1" {
		t.Errorf("expected winner 'bidder-1', got '%s'", result.WinnerID)
	}
}

func TestAuction_NoBids(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	result, err := a.Start(context.Background(), "auction.topic", []string{"bidder-1"}, "item", AuctionConfig{
		Strategy: AuctionHighestBid,
		Timeout:  50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start should not error: %v", err)
	}

	if result.HasWinner() {
		t.Error("expected no winner")
	}
	if result.BidCount != 0 {
		t.Errorf("expected 0 bids, got %d", result.BidCount)
	}
}

func TestAuction_Bid_Helper(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	request := NewBusMessage("topic", "auctioneer", "bidder", "item")
	request.SetMetadata("response_topic", "topic.bid")
	request.SetMetadata("correlation_id", "test-id")

	err := a.Bid(context.Background(), request, 150.0, "I offer this amount")
	if err != nil {
		t.Fatalf("Bid failed: %v", err)
	}

	// Verify the bid was published with correct metadata
	sub, _ := bus.Subscribe([]string{"topic.bid"}, func(ctx context.Context, msg BusMessage) error {
		value, _ := msg.GetMetadata("bid_value")
		proposal, _ := msg.GetMetadata("bid_proposal")

		if value != 150.0 {
			t.Errorf("expected bid value 150.0, got %v", value)
		}
		if proposal != "I offer this amount" {
			t.Errorf("expected proposal 'I offer this amount', got %v", proposal)
		}
		return nil
	})
	defer sub.Unsubscribe()

	time.Sleep(50 * time.Millisecond)
}

func TestAuction_Bid_MissingMetadata(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	// Missing response_topic
	request := NewBusMessage("topic", "auctioneer", "bidder", "item")
	err := a.Bid(context.Background(), request, 100.0, "")
	if err == nil {
		t.Error("expected error when response_topic is missing")
	}

	// Missing correlation_id
	request.SetMetadata("response_topic", "topic.bid")
	err = a.Bid(context.Background(), request, 100.0, "")
	if err == nil {
		t.Error("expected error when correlation_id is missing")
	}
}

func TestAuctionResult_GetBid(t *testing.T) {
	result := &AuctionResult{
		Bids: map[string]Bid{
			"bidder-1": {BidderID: "bidder-1", Value: 100.0},
			"bidder-2": {BidderID: "bidder-2", Value: 200.0},
		},
	}

	bid, err := result.GetBid("bidder-1")
	if err != nil {
		t.Fatalf("GetBid failed: %v", err)
	}
	if bid.Value != 100.0 {
		t.Errorf("expected bid 100.0, got %v", bid.Value)
	}

	_, err = result.GetBid("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent bidder")
	}
}

func TestAuction_BidWithStringPayload(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	bidders := []string{"bidder-1"}

	// Bidder sends string payload instead of numeric
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.Subscribe([]string{"auction.topic"}, func(ctx context.Context, msg BusMessage) error {
			responseTopic, _ := msg.GetMetadata("response_topic")
			correlationID, _ := msg.GetMetadata("correlation_id")

			// Send string that can be parsed as float
			bidMsg := NewBusMessage(responseTopic.(string), "bidder-1", msg.FromAgent, "75.5")
			bidMsg.SetMetadata("correlation_id", correlationID)
			return bus.Publish(ctx, responseTopic.(string), bidMsg)
		})
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := a.Start(context.Background(), "auction.topic", bidders, "item", AuctionConfig{
		Strategy: AuctionHighestBid,
		Timeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if result.HasWinner() {
		// String "75.5" should be parsed
		if result.Winner.Value != 75.5 {
			t.Errorf("expected bid 75.5 from string, got %v", result.Winner.Value)
		}
	}
}

func TestAuction_DuplicateBids(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	a := NewAuction(bus)

	bidders := []string{"bidder-1"}

	var callCount atomic.Int64

	// Bidder sends multiple bids - only first should count
	ready := make(chan struct{})
	go func() {
		sub, _ := bus.Subscribe([]string{"auction.topic"}, func(ctx context.Context, msg BusMessage) error {
			callCount.Add(1)
			responseTopic, _ := msg.GetMetadata("response_topic")
			correlationID, _ := msg.GetMetadata("correlation_id")

			for i := 0; i < 3; i++ {
				bidMsg := NewBusMessage(responseTopic.(string), "bidder-1", msg.FromAgent, float64(i*100))
				bidMsg.SetMetadata("correlation_id", correlationID)
				bidMsg.SetMetadata("bid_value", float64(i*100))
				_ = bus.Publish(ctx, responseTopic.(string), bidMsg)
			}
			return nil
		})
		defer sub.Unsubscribe()
		close(ready) // Signal subscription is ready
		time.Sleep(500 * time.Millisecond)
	}()

	<-ready // Wait for subscription to be ready

	result, err := a.Start(context.Background(), "auction.topic", bidders, "item", AuctionConfig{
		Strategy: AuctionHighestBid,
		Timeout:  200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Should only have 1 bid (first one)
	if result.BidCount != 1 {
		t.Errorf("expected 1 bid (duplicates ignored), got %d", result.BidCount)
	}
}

func TestNewMulticast(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	m := NewMulticast(bus)
	if m == nil {
		t.Fatal("expected Multicast to be created")
	}
}

func TestMulticast_Send_AllSuccess(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	m := NewMulticast(bus)

	targetAgents := []string{"agent-1", "agent-2", "agent-3"}

	var received atomic.Int64
	sub, _ := bus.SubscribeWithFilter(
		[]string{"multicast.topic"},
		func(ctx context.Context, msg BusMessage) error {
			received.Add(1)
			return nil
		},
		func(msg BusMessage) bool {
			return msg.ToAgent != ""
		},
	)
	defer sub.Unsubscribe()

	result, err := m.Send(context.Background(), "multicast.topic", targetAgents, "message")
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if result.SentCount != 3 {
		t.Errorf("expected SentCount 3, got %d", result.SentCount)
	}
	if result.FailedCount != 0 {
		t.Errorf("expected FailedCount 0, got %d", result.FailedCount)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
	if result.MessageID == "" {
		t.Error("expected MessageID to be set")
	}

	time.Sleep(50 * time.Millisecond)
	if received.Load() != 3 {
		t.Errorf("expected 3 messages received, got %d", received.Load())
	}
}

func TestMulticast_Send_EmptyTargets(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	m := NewMulticast(bus)

	result, err := m.Send(context.Background(), "topic", []string{}, "message")
	if err != nil {
		t.Fatalf("Send with empty targets should not error: %v", err)
	}

	if result.SentCount != 0 {
		t.Errorf("expected SentCount 0, got %d", result.SentCount)
	}
}

func TestMulticast_SameMessageID(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	m := NewMulticast(bus)

	var messageIDs []string
	var mu sync.Mutex

	sub, _ := bus.SubscribeWithFilter(
		[]string{"topic"},
		func(ctx context.Context, msg BusMessage) error {
			mu.Lock()
			messageIDs = append(messageIDs, msg.ID)
			mu.Unlock()
			return nil
		},
		func(msg BusMessage) bool {
			return msg.ToAgent != ""
		},
	)
	defer sub.Unsubscribe()

	m.Send(context.Background(), "topic", []string{"agent-1", "agent-2"}, "message")

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(messageIDs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messageIDs))
	}

	// All messages in a multicast should have the same ID
	if messageIDs[0] != messageIDs[1] {
		t.Errorf("expected same message ID for multicast, got %s and %s", messageIDs[0], messageIDs[1])
	}
}

func TestMulticast_Metadata(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	m := NewMulticast(bus)

	var receivedMsg BusMessage
	var mu sync.Mutex

	sub, _ := bus.SubscribeWithFilter(
		[]string{"topic"},
		func(ctx context.Context, msg BusMessage) error {
			mu.Lock()
			receivedMsg = msg
			mu.Unlock()
			return nil
		},
		func(msg BusMessage) bool {
			return msg.ToAgent == "agent-1"
		},
	)
	defer sub.Unsubscribe()

	m.Send(context.Background(), "topic", []string{"agent-1"}, "message")

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	isMulticast, _ := receivedMsg.GetMetadata("multicast")
	if isMulticast != true {
		t.Error("expected multicast metadata to be true")
	}

	multicastID, _ := receivedMsg.GetMetadata("multicast_id")
	if multicastID == "" {
		t.Error("expected multicast_id metadata to be set")
	}
}

func TestMulticast_ConcurrentSend(t *testing.T) {
	bus := NewInMemoryBus()
	defer bus.Close()

	m := NewMulticast(bus)

	var wg sync.WaitGroup
	numMulticasts := 10

	for i := 0; i < numMulticasts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Send(context.Background(), "topic", []string{"agent-1", "agent-2"}, "message")
		}()
	}

	wg.Wait()

	// Should complete without panics or deadlocks
}
