package processor

import (
	"context"
	"fmt"
	"time"

	"redis/internal/storage"
)

type CommandType int

const (
	CmdSet CommandType = iota
	CmdGet
	CmdDelete
	CmdExists
	CmdKeys
	CmdFlush
	CmdCleanup
	CmdExpire
	CmdTTL
	CmdIncr
	CmdIncrBy
	CmdDecr
	CmdDecrBy
	CmdSnapshot     // For AOF rewrite (returns [][]string commands)
	CmdDataSnapshot // For RDB snapshots (returns map[string]*Value)
	// List commands
	CmdLPush
	CmdRPush
	CmdLPop
	CmdRPop
	CmdLLen
	CmdLRange
	CmdLIndex
	CmdLSet
	CmdLRem
	CmdLTrim
	CmdLInsert
	// Hash commands
	CmdHSet
	CmdHGet
	CmdHMGet
	CmdHDel
	CmdHExists
	CmdHLen
	CmdHKeys
	CmdHVals
	CmdHGetAll
	CmdHSetNX
	CmdHIncrBy
	CmdHIncrByFloat
	// Set commands
	CmdSAdd
	CmdSRem
	CmdSIsMember
	CmdSMembers
	CmdSCard
	CmdSPop
	CmdSRandMember
	CmdSUnion
	CmdSInter
	CmdSDiff
	CmdSMove
	CmdSUnionStore
	CmdSInterStore
	CmdSDiffStore
	// Sorted Set commands
	CmdZAdd
	CmdZRem
	CmdZScore
	CmdZRank
	CmdZRevRank
	CmdZCard
	CmdZRange
	CmdZRevRange
	CmdZRangeByScore
	CmdZRevRangeByScore
	CmdZIncrBy
	CmdZCount
	CmdZPopMin
	CmdZPopMax
	CmdZRemRangeByScore
	CmdZRemRangeByRank
	// Geospatial commands
	CmdGeoAdd
	CmdGeoPos
	CmdGeoDist
	CmdGeoHash
	CmdGeoRadius
	CmdGeoRadiusByMember
	// Bloom Filter commands
	CmdBFReserve
	CmdBFAdd
	CmdBFMAdd
	CmdBFExists
	CmdBFMExists
	CmdBFInfo
	// HyperLogLog commands
	CmdPFAdd
	CmdPFCount
	CmdPFMerge
	// Bitmap commands
	CmdSetBit
	CmdGetBit
	CmdBitCount
	CmdBitPos
	CmdBitOp
	// Pub/Sub commands
	CmdPublish
	CmdPubSubChannels
	CmdPubSubNumSub
	CmdPubSubNumPat
	CmdSubscribe
	CmdUnsubscribe
	CmdPSubscribe
	CmdPUnsubscribe
)

// Result types for command responses
type IntResult struct {
	Result int
	Err    error
}

type StringSliceResult struct {
	Result []string
	Err    error
}

type IndexResult struct {
	Value  string
	Exists bool
	Err    error
}

type GetResult struct {
	Value  interface{}
	Exists bool
}

type Int64Result struct {
	Result int64
	Err    error
}

type Float64Result struct {
	Result float64
	Err    error
}

type BoolResult struct {
	Result bool
	Err    error
}

type StringResult struct {
	Result string
	Err    error
}

type BoolSliceResult struct {
	Results []bool
	Err     error
}

type InterfaceSliceResult struct {
	Result []interface{}
	Err    error
}

type Command struct {
	Type     CommandType
	Key      string
	Value    interface{}
	Expiry   *time.Time
	Args     []interface{} // Additional arguments for complex commands
	ClientID int64         // Client ID for pub/sub subscriptions
	Response chan interface{}
}

// GetSubscriberID returns a string representation of the client ID for pub/sub
func (c *Command) GetSubscriberID() string {
	if c.ClientID == 0 {
		return "default"
	}
	return fmt.Sprintf("client:%d", c.ClientID)
}

// CommandExecutor is a function type for command executors
type CommandExecutor func(cmd *Command)

type Processor struct {
	store       *storage.Store
	commandChan chan *Command
	ctx         context.Context
	cancel      context.CancelFunc
	executors   map[CommandType]CommandExecutor
}

func NewProcessor(store *storage.Store) *Processor {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Processor{
		store:       store,
		commandChan: make(chan *Command, 1000),
		ctx:         ctx,
		cancel:      cancel,
	}
	p.registerExecutors()
	go p.run()
	go p.periodicCleanup()
	return p
}

// GetStore returns the underlying store (for pub/sub cleanup)
func (p *Processor) GetStore() *storage.Store {
	return p.store
}

// registerExecutors initializes the executor map
func (p *Processor) registerExecutors() {
	p.executors = make(map[CommandType]CommandExecutor)

	// String/Basic commands
	p.registerStringExecutors()

	// List commands
	p.registerListExecutors()

	// Hash commands
	p.registerHashExecutors()

	// Set commands
	p.registerSetExecutors()

	// Sorted Set commands
	p.registerZSetExecutors()

	// Geospatial commands
	p.registerGeoExecutors()

	// Bloom Filter commands
	p.registerBloomExecutors()

	// HyperLogLog commands
	p.registerHyperLogLogExecutors()

	// Bitmap commands
	p.registerBitmapExecutors()

	// Pub/Sub commands
	p.registerPubSubExecutors()

	// Snapshot commands for AOF rewrite and RDB snapshots
	p.executors[CmdSnapshot] = p.executeSnapshot
	p.executors[CmdDataSnapshot] = p.executeDataSnapshot
}

// registerStringExecutors registers string command executors
func (p *Processor) registerStringExecutors() {
	stringCmds := []CommandType{
		CmdSet, CmdGet, CmdDelete, CmdExists,
		CmdKeys, CmdFlush, CmdCleanup, CmdExpire, CmdTTL,
		CmdIncr, CmdIncrBy, CmdDecr, CmdDecrBy,
	}
	for _, cmdType := range stringCmds {
		p.executors[cmdType] = p.executeStringCommand
	}
}

// registerListExecutors registers list command executors
func (p *Processor) registerListExecutors() {
	listCmds := []CommandType{
		CmdLPush, CmdRPush, CmdLPop, CmdRPop, CmdLLen,
		CmdLRange, CmdLIndex, CmdLSet, CmdLRem, CmdLTrim, CmdLInsert,
	}
	for _, cmdType := range listCmds {
		p.executors[cmdType] = p.executeListCommand
	}
}

// registerHashExecutors registers hash command executors
func (p *Processor) registerHashExecutors() {
	hashCmds := []CommandType{
		CmdHSet, CmdHGet, CmdHMGet, CmdHDel, CmdHExists,
		CmdHLen, CmdHKeys, CmdHVals, CmdHGetAll, CmdHSetNX,
		CmdHIncrBy, CmdHIncrByFloat,
	}
	for _, cmdType := range hashCmds {
		p.executors[cmdType] = p.executeHashCommand
	}
}

// registerSetExecutors registers set command executors
func (p *Processor) registerSetExecutors() {
	setCmds := []CommandType{
		CmdSAdd, CmdSRem, CmdSIsMember, CmdSMembers, CmdSCard,
		CmdSPop, CmdSRandMember, CmdSUnion, CmdSInter, CmdSDiff,
		CmdSMove, CmdSUnionStore, CmdSInterStore, CmdSDiffStore,
	}
	for _, cmdType := range setCmds {
		p.executors[cmdType] = p.executeSetCommand
	}
}

// registerZSetExecutors registers sorted set command executors
func (p *Processor) registerZSetExecutors() {
	zsetCmds := []CommandType{
		CmdZAdd, CmdZRem, CmdZScore, CmdZRank, CmdZRevRank,
		CmdZCard, CmdZRange, CmdZRevRange, CmdZRangeByScore, CmdZRevRangeByScore,
		CmdZIncrBy, CmdZCount, CmdZPopMin, CmdZPopMax,
		CmdZRemRangeByScore, CmdZRemRangeByRank,
	}
	for _, cmdType := range zsetCmds {
		p.executors[cmdType] = p.executeZSetCommand
	}
}

// registerGeoExecutors registers geospatial command executors
func (p *Processor) registerGeoExecutors() {
	geoCmds := []CommandType{
		CmdGeoAdd, CmdGeoPos, CmdGeoDist, CmdGeoHash,
		CmdGeoRadius, CmdGeoRadiusByMember,
	}
	for _, cmdType := range geoCmds {
		p.executors[cmdType] = p.executeGeoCommand
	}
}

// registerBloomExecutors registers Bloom filter command executors
func (p *Processor) registerBloomExecutors() {
	bloomCmds := []CommandType{
		CmdBFReserve, CmdBFAdd, CmdBFMAdd,
		CmdBFExists, CmdBFMExists, CmdBFInfo,
	}
	for _, cmdType := range bloomCmds {
		p.executors[cmdType] = p.executeBloomCommand
	}
}

// registerHyperLogLogExecutors registers HyperLogLog command executors
func (p *Processor) registerHyperLogLogExecutors() {
	hllCmds := []CommandType{
		CmdPFAdd, CmdPFCount, CmdPFMerge,
	}
	for _, cmdType := range hllCmds {
		p.executors[cmdType] = p.executeHyperLogLogCommand
	}
}

// registerBitmapExecutors registers bitmap command executors
func (p *Processor) registerBitmapExecutors() {
	bitmapCmds := []CommandType{
		CmdSetBit, CmdGetBit, CmdBitCount, CmdBitPos, CmdBitOp,
	}
	for _, cmdType := range bitmapCmds {
		p.executors[cmdType] = p.executeBitmapCommand
	}
}

// registerPubSubExecutors registers pub/sub command executors
func (p *Processor) registerPubSubExecutors() {
	pubsubCmds := []CommandType{
		CmdPublish, CmdPubSubChannels, CmdPubSubNumSub, CmdPubSubNumPat,
		CmdSubscribe, CmdUnsubscribe, CmdPSubscribe, CmdPUnsubscribe,
	}
	for _, cmdType := range pubsubCmds {
		p.executors[cmdType] = p.executePubSubCommand
	}
}

func (p *Processor) run() {
	for {
		select {
		case <-p.ctx.Done():
			// Drain remaining commands before exiting
			p.drainCommands()
			return
		case cmd := <-p.commandChan:
			p.executeCommand(cmd)
		}
	}
}

func (p *Processor) drainCommands() {
	for {
		select {
		case cmd := <-p.commandChan:
			p.executeCommand(cmd)
		default:
			// Channel empty
			return
		}
	}
}

func (p *Processor) executeCommand(cmd *Command) {
	if executor, exists := p.executors[cmd.Type]; exists {
		executor(cmd)
	}
}

func (p *Processor) periodicCleanup() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			cmd := &Command{
				Type:     CmdCleanup,
				Response: make(chan interface{}, 1),
			}
			p.commandChan <- cmd
			<-cmd.Response
		}
	}
}

func (p *Processor) Submit(cmd *Command) {
	p.commandChan <- cmd
}

func (p *Processor) Shutdown() {
	p.cancel()
	close(p.commandChan)
}

// Direct methods for blocking operations
// These submit commands and wait for results synchronously

// LPop removes and returns the first element from a list
func (p *Processor) LPop(key string) (string, bool) {
	cmd := &Command{
		Type:     CmdLPop,
		Key:      key,
		Args:     []interface{}{1}, // Pop 1 element
		Response: make(chan interface{}, 1),
	}
	p.Submit(cmd)
	result := <-cmd.Response

	res := result.(StringSliceResult)
	if res.Err != nil || len(res.Result) == 0 {
		return "", false
	}
	return res.Result[0], true
}

// RPop removes and returns the last element from a list
func (p *Processor) RPop(key string) (string, bool) {
	cmd := &Command{
		Type:     CmdRPop,
		Key:      key,
		Args:     []interface{}{1}, // Pop 1 element
		Response: make(chan interface{}, 1),
	}
	p.Submit(cmd)
	result := <-cmd.Response

	res := result.(StringSliceResult)
	if res.Err != nil || len(res.Result) == 0 {
		return "", false
	}
	return res.Result[0], true
}

// LPush adds elements to the head of a list
func (p *Processor) LPush(key string, values []string) int {
	cmd := &Command{
		Type:     CmdLPush,
		Key:      key,
		Args:     []interface{}{values},
		Response: make(chan interface{}, 1),
	}
	p.Submit(cmd)
	result := <-cmd.Response

	res := result.(IntResult)
	if res.Err != nil {
		return 0
	}
	return res.Result
}

// RPush adds elements to the tail of a list
func (p *Processor) RPush(key string, values []string) int {
	cmd := &Command{
		Type:     CmdRPush,
		Key:      key,
		Args:     []interface{}{values},
		Response: make(chan interface{}, 1),
	}
	p.Submit(cmd)
	result := <-cmd.Response

	res := result.(IntResult)
	if res.Err != nil {
		return 0
	}
	return res.Result
}

// LLen returns the length of a list
func (p *Processor) LLen(key string) int {
	cmd := &Command{
		Type:     CmdLLen,
		Key:      key,
		Response: make(chan interface{}, 1),
	}
	p.Submit(cmd)
	result := <-cmd.Response

	res := result.(IntResult)
	if res.Err != nil {
		return 0
	}
	return res.Result
}

// GetSnapshot returns a snapshot of all data as raw storage data for AOF rewrite
// Returns shallow copy with COW - filtering and conversion happens in background
func (p *Processor) GetSnapshot() map[string]*storage.Value {
	cmd := &Command{
		Type:     CmdSnapshot,
		Response: make(chan interface{}, 1),
	}
	p.Submit(cmd)
	result := <-cmd.Response
	return result.(map[string]*storage.Value)
}

// GetDataSnapshot returns a shallow copy snapshot of raw storage data for RDB snapshots
// This is used by BGSAVE to get the actual data structures, not command representations
// Uses copy-on-write optimization - MUST call ReleaseSnapshot() when done!
func (p *Processor) GetDataSnapshot() map[string]*storage.Value {
	cmd := &Command{
		Type:     CmdDataSnapshot,
		Response: make(chan interface{}, 1),
	}
	p.Submit(cmd)
	result := <-cmd.Response
	return result.(map[string]*storage.Value)
}

// ReleaseSnapshot decrements the snapshot reference counter (COW optimization)
// MUST be called after snapshot operations complete (AOF rewrite, BGSAVE)
func (p *Processor) ReleaseSnapshot() {
	p.store.ReleaseSnapshot()
}
