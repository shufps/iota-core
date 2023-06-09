package drr

import (
	"container/heap"
	"fmt"

	"go.uber.org/atomic"

	"github.com/iotaledger/hive.go/ds/generalheap"
	"github.com/iotaledger/hive.go/ds/shrinkingmap"
	"github.com/iotaledger/hive.go/runtime/timed"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/blocks"

	iotago "github.com/iotaledger/iota.go/v4"
)

// region IssuerQueue /////////////////////////////////////////////////////////////////////////////////////////////

// IssuerQueue keeps the submitted blocks of an issuer.
type IssuerQueue struct {
	issuerID  iotago.AccountID
	submitted *shrinkingmap.ShrinkingMap[iotago.BlockID, *blocks.Block]
	inbox     generalheap.Heap[timed.HeapKey, *blocks.Block]
	size      atomic.Int64
	work      atomic.Int64
}

// NewIssuerQueue returns a new IssuerQueue.
func NewIssuerQueue(issuerID iotago.AccountID) *IssuerQueue {
	return &IssuerQueue{
		issuerID:  issuerID,
		submitted: shrinkingmap.New[iotago.BlockID, *blocks.Block](),
	}
}

// Size returns the total number of blocks in the queue.
// This function is thread-safe.
func (q *IssuerQueue) Size() int {
	if q == nil {
		return 0
	}
	return int(q.size.Load())
}

// Work returns the total work of the blocks in the queue.
// This function is thread-safe.
func (q *IssuerQueue) Work() int {
	if q == nil {
		return 0
	}
	return int(q.work.Load())
}

// IssuerID returns the ID of the issuer belonging to the queue.
func (q *IssuerQueue) IssuerID() iotago.AccountID {
	return q.issuerID
}

// Submit submits a block for the queue.
func (q *IssuerQueue) Submit(element *blocks.Block) bool {
	// this is just a debugging check, it will never happen in practice
	if blkIssuerID := element.Block().IssuerID; q.issuerID != blkIssuerID {
		panic(fmt.Sprintf("issuerqueue: queue issuer ID(%x) and issuer ID(%x) does not match.", q.issuerID, blkIssuerID))
	}

	if _, submitted := q.submitted.Get(element.ID()); submitted {
		return false
	}

	q.submitted.Set(element.ID(), element)
	q.size.Inc()
	q.work.Add(int64(element.Work()))
	return true
}

// Unsubmit removes a previously submitted block from the queue.
func (q *IssuerQueue) Unsubmit(block *blocks.Block) bool {
	if _, submitted := q.submitted.Get(block.ID()); !submitted {
		return false
	}

	q.submitted.Delete(block.ID())
	q.size.Dec()
	q.work.Sub(int64(block.Work()))
	return true
}

// Ready marks a previously submitted block as ready to be scheduled.
func (q *IssuerQueue) Ready(block *blocks.Block) bool {
	if _, submitted := q.submitted.Get(block.ID()); !submitted {
		return false
	}

	q.submitted.Delete(block.ID())
	heap.Push(&q.inbox, &generalheap.HeapElement[timed.HeapKey, *blocks.Block]{Value: block, Key: timed.HeapKey(block.IssuingTime())})
	return true
}

// IDs returns the IDs of all submitted blocks (ready or not).
func (q *IssuerQueue) IDs() (ids []iotago.BlockID) {
	q.submitted.ForEachKey(func(id iotago.BlockID) bool {
		ids = append(ids, id)
		return true
	})

	for _, block := range q.inbox {
		ids = append(ids, block.Value.ID())
	}
	return ids
}

// Front returns the first ready block in the queue.
func (q *IssuerQueue) Front() *blocks.Block {
	if q == nil || q.inbox.Len() == 0 {
		return nil
	}
	return q.inbox[0].Value
}

// PopFront removes the first ready block from the queue.
func (q *IssuerQueue) PopFront() *blocks.Block {
	blk := heap.Pop(&q.inbox).(*generalheap.HeapElement[timed.HeapKey, *blocks.Block]).Value
	q.size.Dec()
	q.work.Sub(int64(blk.Work()))
	return blk
}

func (q IssuerQueue) RemoveTail() {
	tail := q.tail()
	heap.Remove(&q.inbox, tail)
}

func (q IssuerQueue) tail() (tail int) {
	h := q.inbox
	for i := range h {
		if !h.Less(i, tail) { // less means older issue time
			tail = i
		}
	}
	return
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
