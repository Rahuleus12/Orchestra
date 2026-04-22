# Phase 5 — Inter-Agent Communication

## Completion Report

## Executive Summary

Phase 5 implements inter-agent communication via an in-process publish-subscribe message bus. This enables agents to communicate outside of rigid workflow structures through topic-based messaging, direct agent-to-agent messaging, and advanced communication patterns like broadcast, consensus, and auction.

The implementation provides a thread-safe, backpressure-aware messaging system with support for:
- Topic-based subscriptions with wildcard matching
- Per-agent mailboxes with configurable overflow behavior
- Synchronous request/response patterns with correlation IDs
- Broadcast with response aggregation
- Consensus voting (majority, unanimous, weighted)
- Competitive auction (highest bid, lowest bid, first bid)
- Multicast to specific agent sets

## Deliverables Checklist

### 5.1 Message Bus ✅

**File:** `internal/bus/bus.go`

Implemented an in-memory publish-subscribe message bus with the following features:

- **Topic-based subscriptions:** Agents subscribe to topics and receive all messages published to those topics
- **Wildcard matching:** Patterns like `events.*`, `agent.*.status` match multiple topics
- **Direct agent messaging:** Messages with `ToAgent` set are targeted to specific agents
- **Message filtering:** Subscriptions can include filters to further refine which messages are received
- **Thread-safe:** All operations are protected by mutexes, safe for concurrent use
- **Asynchronous delivery:** Messages are delivered via goroutines to avoid blocking publishers
- **Backpressure handling:** Configurable handler timeout prevents slow handlers from blocking the bus
- **Statistics tracking:** Publish/deliver/drop/error counts for observability

**Key types:**
- `Bus` interface - the public API for message buses
- `InMemoryBus` - the in-process implementation
- `BusMessage` - message structure with ID, topic, sender, recipient, payload, metadata
- `Subscription` - represents an active subscription
- `Handler` - function type for message handlers
- `MessageFilter` - function type for message filtering
- `BusStats` - statistics about bus operations

### 5.2 Agent Mailbox ✅

**File:** `internal/bus/mailbox.go`

Implemented per-agent mailboxes for direct message reception:

- **Buffered queue:** Configurable capacity with multiple overflow behaviors
- **Overflow behaviors:**
  - `MailboxDropOldest` - removes oldest message (default)
  - `MailboxDropNewest` - rejects new messages
  - `MailboxBlock` - blocks until space available (falls back to drop oldest)
  - `MailboxGrow` - dynamically doubles capacity up to MaxCapacity
- **Blocking and non-blocking receive:** `Receive()`, `TryReceive()`, `ReceiveN()`
- **Filtered receive:** `ReceiveWithFilter()` blocks until a matching message arrives
- **Peek operations:** View messages without removing them
- **Inbound filtering:** Reject messages before they enter the queue
- **Bus integration:** `ConnectToBus()` and `ConnectToBusTopic()` for automatic message routing
- **Statistics tracking:** Received/consumed/dropped/filtered counts

**Key types:**
- `Mailbox` - per-agent message inbox
- `MailboxStats` - statistics about mailbox operations
- `MailboxError` - typed errors for different failure modes
- `MailboxFilter` - function type for inbound filtering
- Pre-built filters: `FilterFromAgent()`, `FilterByTopic()`, `FilterByTopicPrefix()`, `FilterNotFromAgent()`, `FilterWithMetadata()`, `FilterCombine()`, `FilterAny()`

### 5.3 Broadcast Patterns ✅

**File:** `internal/bus/patterns.go`

Implemented advanced communication patterns on top of the message bus:

#### Request-Response Pattern
- Synchronous agent-to-agent communication with timeout
- Correlation IDs link requests to responses
- `Request()` sends and waits; `Respond()` replies

#### Request-Broadcast Pattern
- One agent asks, many agents answer
- Collects all responses with timeout
- `BroadcastResult` provides `ResponseCount()`, `AllResponded()`, `GetResponse()`

#### Consensus Pattern
- Voting-based decision making among agents
- **Quorum strategies:**
  - `QuorumMajority` - more than half the weight agrees
  - `QuorumAll` - all agents must vote
  - `QuorumSimple` - any response accepted
  - `QuorumThreshold` - minimum weight threshold
- **Weighted voting:** Agents can have different vote weights
- `ConsensusResult` provides `Winner`, `Tally`, `Majority()`, `Unanimous()`

#### Auction Pattern
- Competitive bidding among agents
- **Strategies:**
  - `AuctionHighestBid` - highest value wins
  - `AuctionLowestBid` - lowest value wins (for cost minimization)
  - `AuctionFirstBid` - first response wins
- Configurable minimum bid count
- Duplicate bids ignored (first bid per agent counts)
- `AuctionResult` provides `Winner`, `WinnerID`, `HasWinner()`, `GetBid()`

#### Multicast Pattern
- Send to specific set of agents (vs broadcast to all subscribers)
- Parallel delivery with error tracking
- All messages in a multicast share the same ID
- `MulticastResult` provides `SentCount`, `FailedCount`, `Errors`

## Code Statistics

### Lines of Code

| Component | Lines |
|-----------|-------|
| bus.go | 556 |
| mailbox.go | 750 |
| patterns.go | 1100 |
| **Total implementation** | **2406** |

### Test Files

| File | Tests |
|------|-------|
| bus_test.go | 45 |
| mailbox_test.go | 55 |
| patterns_test.go | 50 |
| **Total tests** | **150** |

### Files Created

| File | Purpose |
|------|---------|
| `internal/bus/bus.go` | Core message bus implementation |
| `internal/bus/mailbox.go` | Agent mailbox implementation |
| `internal/bus/patterns.go` | Communication patterns |
| `internal/bus/bus_test.go` | Bus tests |
| `internal/bus/mailbox_test.go` | Mailbox tests |
| `internal/bus/patterns_test.go` | Pattern tests |

## Test Results

### All Tests Passing

```
ok   github.com/user/orchestra/internal/bus    4.551s
```

Full test suite verified:
```
ok   github.com/user/orchestra/internal/agent          2.347s
ok   github.com/user/orchestra/internal/bus            4.551s
ok   github.com/user/orchestra/internal/config         1.413s
ok   github.com/user/orchestra/internal/message        1.153s
ok   github.com/user/orchestra/internal/middleware     16.563s
ok   github.com/user/orchestra/internal/orchestration  1.034s
ok   github.com/user/orchestra/internal/provider       0.822s
ok   github.com/user/orchestra/internal/provider/mock  0.893s
```

## Public API Surface

### Bus Construction

```go
func NewInMemoryBus(opts ...BusOption) *InMemoryBus
func WithBusLogger(logger *slog.Logger) BusOption
func WithBufferSize(size int) BusOption
func WithHandlerTimeout(timeout time.Duration) BusOption
```

### Bus Interface

```go
type Bus interface {
    Publish(ctx context.Context, topic string, msg BusMessage) error
    Subscribe(topics []string, handler Handler) (*Subscription, error)
    SubscribeWithFilter(topics []string, handler Handler, filter MessageFilter) (*Subscription, error)
    Unsubscribe(sub *Subscription) error
    Close() error
    Stats() BusStats
}
```

### Message Types

```go
type BusMessage struct { ... }
func NewBusMessage(topic, fromAgent, toAgent string, payload any) BusMessage
func (m BusMessage) IsBroadcast() bool
func (m BusMessage) IsTargeted() bool
func (m *BusMessage) SetMetadata(key string, value any)
func (m BusMessage) GetMetadata(key string) (any, bool)
func (m BusMessage) Clone() BusMessage

type Handler func(ctx context.Context, msg BusMessage) error
type MessageFilter func(msg BusMessage) bool
```

### Subscription

```go
type Subscription struct { ... }
func (s *Subscription) ID() string
func (s *Subscription) Topics() []string
func (s *Subscription) Active() bool
func (s *Subscription) Unsubscribe() error
```

### Mailbox Construction

```go
func NewMailbox(agentID string, opts ...MailboxOption) *Mailbox
func WithMailboxLogger(logger *slog.Logger) MailboxOption
func WithMailboxCapacity(capacity int) MailboxOption
func WithMailboxBehavior(behavior MailboxBehavior) MailboxOption
func WithMailboxFilter(filter MailboxFilter) MailboxOption
func WithOnMessage(callback func(msg BusMessage)) MailboxOption
```

### Mailbox Operations

```go
func (m *Mailbox) AgentID() string
func (m *Mailbox) Receive(ctx context.Context) (BusMessage, error)
func (m *Mailbox) ReceiveWithTimeout(timeout time.Duration) (BusMessage, error)
func (m *Mailbox) TryReceive() (BusMessage, error)
func (m *Mailbox) ReceiveN(n int) []BusMessage
func (m *Mailbox) ReceiveWithFilter(ctx context.Context, filter MailboxFilter) (BusMessage, error)
func (m *Mailbox) Peek() (BusMessage, error)
func (m *Mailbox) PeekN(n int) []BusMessage
func (m *Mailbox) Send(msg BusMessage) error
func (m *Mailbox) Size() int
func (m *Mailbox) Capacity() int
func (m *Mailbox) IsEmpty() bool
func (m *Mailbox) IsFull() bool
func (m *Mailbox) Clear() int
func (m *Mailbox) Close() error
func (m *Mailbox) Stats() MailboxStats
func (m *Mailbox) ConnectToBus(bus Bus) error
func (m *Mailbox) ConnectToBusTopic(bus Bus, topic string) error
func (m *Mailbox) DisconnectFromBus()
func (m *Mailbox) HasMessagesFrom(agentID string) bool
func (m *Mailbox) CountMessagesFrom(agentID string) int
```

### Mailbox Filters

```go
func FilterFromAgent(agentID string) MailboxFilter
func FilterByTopic(topic string) MailboxFilter
func FilterByTopicPrefix(prefix string) MailboxFilter
func FilterNotFromAgent(agentID string) MailboxFilter
func FilterWithMetadata(key string, value any) MailboxFilter
func FilterCombine(filters ...MailboxFilter) MailboxFilter
func FilterAny(filters ...MailboxFilter) MailboxFilter
```

### Request-Response Pattern

```go
func NewRequestResponse(bus Bus) *RequestResponse
func (rr *RequestResponse) Request(ctx context.Context, toAgent, topic string, payload any, timeout time.Duration) (BusMessage, error)
func (rr *RequestResponse) Respond(ctx context.Context, request BusMessage, payload any) error
```

### Broadcast Pattern

```go
func NewRequestBroadcast(bus Bus) *RequestBroadcast
func (rb *RequestBroadcast) Broadcast(ctx context.Context, topic string, targetAgents []string, payload any, timeout time.Duration) (*BroadcastResult, error)

type BroadcastResult struct { ... }
func (r *BroadcastResult) ResponseCount() int
func (r *BroadcastResult) AllResponded() bool
func (r *BroadcastResult) GetResponse(agentID string) (BusMessage, error)
```

### Consensus Pattern

```go
func NewConsensus(bus Bus) *Consensus
func (c *Consensus) Propose(ctx context.Context, topic string, voters []string, proposal any, config ConsensusConfig) (*ConsensusResult, error)
func (c *Consensus) Vote(ctx context.Context, request BusMessage, value string, reason string) error

type ConsensusConfig struct { ... }
func DefaultConsensusConfig() ConsensusConfig

type ConsensusResult struct { ... }
func (r *ConsensusResult) VoteCount() int
func (r *ConsensusResult) Majority() bool
func (r *ConsensusResult) Unanimous() bool
```

### Auction Pattern

```go
func NewAuction(bus Bus) *Auction
func (a *Auction) Start(ctx context.Context, topic string, bidders []string, item any, config AuctionConfig) (*AuctionResult, error)
func (a *Auction) Bid(ctx context.Context, request BusMessage, value float64, proposal string) error

type AuctionConfig struct { ... }
func DefaultAuctionConfig() AuctionConfig

type AuctionResult struct { ... }
func (r *AuctionResult) HasWinner() bool
func (r *AuctionResult) GetBid(bidderID string) (Bid, error)
```

### Multicast Pattern

```go
func NewMulticast(bus Bus) *Multicast
func (m *Multicast) Send(ctx context.Context, topic string, targetAgents []string, payload any) (*MulticastResult, error)

type MulticastResult struct { ... }
```

## Design Decisions

### TDR-022: Channel-Based Notification for Mailbox
Used a buffered notification channel instead of `sync.Cond` to signal when messages are available. This avoids the complexity of condition variables with read-write locks and provides cleaner cancellation semantics.

### TDR-023: Correlation IDs for Request-Response
Request-response patterns use correlation IDs stored in message metadata to match responses to requests. This allows multiple concurrent requests without ambiguity.

### TDR-024: Response Topics Derived from Request Topics
Response topics are automatically derived by appending `.response`, `.vote`, or `.bid` to the request topic. This keeps the topic hierarchy organized and predictable.

### TDR-025: First-Bid Wins for Duplicate Bids
In auctions, only the first bid from each agent is accepted. Subsequent bids are silently ignored to prevent gaming the system.

### TDR-026: Quorum Returns Early
Consensus operations return as soon as quorum is reached, without waiting for all votes. This reduces latency when the outcome is already determined.

## Known Limitations

### Phase 5 Scope
- Only in-memory bus implementation; no Redis/NATS backend for distributed deployments
- No message persistence; messages are lost if the process crashes
- No dead letter queue for failed deliveries

### Bus-Specific Notes
- Handler timeouts are per-message, not cumulative
- Message order within a topic is not guaranteed when multiple publishers exist
- Wildcard matching uses `*` to match any number of segments (not `**`)

### Mailbox-Specific Notes
- `MailboxBlock` behavior falls back to drop-oldest due to mutex constraints
- No priority queue; messages are strictly FIFO

### Pattern-Specific Notes
- Consensus and auction results may have race conditions with very short timeouts
- Request-response requires the responder to be subscribed before the request is sent

## Milestone Criteria Verification

### From PLAN.md — Phase 5 Deliverables

- [x] In-process message bus implementation
- [x] Agent mailbox with subscription filtering
- [x] Broadcast, multicast, and unicast patterns
- [x] Request/response pattern with timeout
- [x] Integration with orchestration engine (mailbox can connect to bus)

### From PLAN.md — Milestone Criteria

- [x] Agents can publish and subscribe to topics
- [x] Direct agent-to-agent messaging works
- [x] Broadcast with aggregation collects responses from multiple agents
- [x] Bus handles backpressure without deadlocking

## Examples

### Basic Pub/Sub

```go
bus := NewInMemoryBus()
defer bus.Close()

// Subscribe to a topic
sub, _ := bus.Subscribe([]string{"events"}, func(ctx context.Context, msg BusMessage) error {
    fmt.Printf("Received: %v\n", msg.Payload)
    return nil
})
defer sub.Unsubscribe()

// Publish a message
bus.Publish(context.Background(), "events", NewBusMessage("events", "sender", "", "hello"))
```

### Agent Mailbox

```go
bus := NewInMemoryBus()
mailbox := NewMailbox("agent-1", WithMailboxCapacity(50))
mailbox.ConnectToBus(bus)
defer mailbox.Close()

// Receive messages (blocks until available)
msg, err := mailbox.Receive(context.Background())

// Filter by sender
msg, err = mailbox.ReceiveWithFilter(ctx, FilterFromAgent("agent-2"))
```

### Request-Response

```go
rr := NewRequestResponse(bus)

// Responder (in a goroutine)
sub, _ := bus.Subscribe([]string{"queries"}, func(ctx context.Context, msg BusMessage) error {
    return rr.Respond(ctx, msg, "the answer")
})

// Requester
resp, err := rr.Request(ctx, "responder", "queries", "what is 42?", 5*time.Second)
```

### Consensus Voting

```go
consensus := NewConsensus(bus)

result, err := consensus.Propose(ctx, "decisions", voters, "Should we proceed?", ConsensusConfig{
    Strategy: QuorumMajority,
    Timeout:  30 * time.Second,
})

if result.Majority() {
    fmt.Printf("Decision: %s won with %v votes\n", result.Winner, result.WinnerWeight)
}
```

### Auction

```go
auction := NewAuction(bus)

result, err := auction.Start(ctx, "task-auction", bidders, taskDescription, AuctionConfig{
    Strategy: AuctionLowestBid,  // Lowest cost wins
    MinBids:  3,
    Timeout:  30 * time.Second,
})

if result.HasWinner() {
    fmt.Printf("Winner: %s with bid %v\n", result.WinnerID, result.Winner.Value)
}
```

## Next Steps: Phase 6 — Tool System & Function Calling

Phase 6 will implement the tool system for agent function calling:

| Task | Description | Priority |
|------|-------------|----------|
| 6.1 Tool Interface & Registry | Define tool interface and registry | High |
| 6.2 Tool Execution | Execute tools and return results | High |
| 6.3 Built-in Tools | File operations, shell, HTTP, search | Medium |
| 6.4 Tool Helper Utilities | Schema generation, validation | Medium |

### Prerequisites for Phase 6
- ✅ Phase 5 message bus complete
- Agent execution loop with tool call support (already in Phase 3)
- Provider interface supports tool definitions (already in Phase 1)

## Conclusion

Phase 5 successfully implements inter-agent communication through a flexible, extensible message bus system. The implementation provides:

1. **Core messaging**: Thread-safe pub/sub with topic-based routing and wildcard matching
2. **Agent mailboxes**: Per-agent message queues with configurable overflow behavior
3. **Communication patterns**: Request-response, broadcast, consensus, auction, and multicast

The system is designed to integrate seamlessly with the existing orchestration engine (Phase 4) and provides the foundation for more complex multi-agent interactions in future phases.