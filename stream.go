package stream

import (
	"reflect"
)

// sink links different stages in a stream
// stream operations should implement this interface to perform data processing
type sink interface {
	// begin should be call before send data to current stage
	begin(size int)
	// end is used to notify current stage that data sending is done
	end()
	// accept takes data from previous stage, and may process it
	accept(t interface{})
	// cancellationRequested indicates that whether current stage already finished processing
	cancellationRequested() bool
}

type FilterFunc func(interface{}) bool
type MapFunc func(interface{}) interface{}
type FlatMapFunc func(interface{}) []interface{}
type ForEachFunc func(interface{})
type GroupFunc func(interface{}) interface{}
type DistinctFunc func(interface{}) interface{}

// ComparatorFunc compares two elements, if a < b return -1, if
// a = b return 0, if a > b return 1
type ComparatorFunc func(a interface{}, b interface{}) int

type ReduceFunc func(in []interface{}, out interface{}) error

// Stream defines all possible stream operations
type Stream interface {
	// Filter uses a FilterFunc to filter out data
	Filter(filter FilterFunc) Stream
	// Map transform data to another shape uses given MapFunc
	Map(mapper MapFunc) Stream
	// FlatMap transform datum to multiple data uses given FlatMapFunc
	FlatMap(mapper FlatMapFunc) Stream
	// Distinct passes only the different data to next stage use a golang built in map
	// the order of data passes to next stage is not guaranteed
	Distinct() Stream
	DistinctByFunc(fn DistinctFunc) Stream
	// Skip will not pass the first n elements it received to next stage
	Skip(n int) Stream
	// Limit will guarantee that no more than n elements pass to next stage
	Limit(n int) Stream
	// Sort uses a given ComparatorFunc to sort data
	Sort(comparator ComparatorFunc) Stream
	// Group uses a given GroupFunc to split data into multiple groups
	// the order of data passes to next stage is not guaranteed
	Group(grouper GroupFunc) Stream
	// Parallel convert a Stream into paralleled Stream, uses parallel go routine to process Stream function
	Parallel() Stream
	// ForEach will call the given ForEachFunc to every element it received
	ForEach(foeEach ForEachFunc)
	// Collect transform stream to array
	Collect() []interface{}
	// Count givens the count of elements in a stream
	Count() int
	// Max returns the maximum element in stream use the given ComparatorFunc
	Max(comparator ComparatorFunc) interface{}
	// Min returns the minimal element in stream use the given ComparatorFunc
	Min(comparator ComparatorFunc) interface{}
	// First returns the first element in stream or nil
	First() interface{}
	// Last returns the last element in stream or nil
	Last() interface{}
	// Reduce uses the ReduceFunc to collect elements in stream
	Reduce(into ReduceFunc, out interface{}) error
}

// stage is the abstraction of a stream stage
type stage interface {
	sink
	Stream
	getStartStage() *startOp
	getNextSink() sink
	setStartStage(s *startOp)
	setNextSink(s sink)
}

// baseStage implements stage, defines the default behavior of a stage
// a stream stage should only override sink interface to perform it's
// own behaviors
type baseStage struct {
	startStage *startOp
	downStream sink
	paralleled bool
}

// New wraps the given data array into Stream
func New(data interface{}) Stream {
	stream := &startOp{}
	setStreamData(stream, data)
	stream.startStage = stream
	return stream
}

// Of provides a convenient way to wrap varargs into Stream
func Of(elements ...interface{}) Stream {
	return New(elements)
}

func setStreamData(stream *startOp, data interface{}) Stream {
	arrValue := reflect.ValueOf(data)
	if arrValue.Kind() == reflect.Ptr {
		arrValue = arrValue.Elem()
	}
	switch arrValue.Kind() {
	case reflect.Slice, reflect.Array:
		data := make([]interface{}, 0, arrValue.Len())
		dataValue := reflect.ValueOf(&data).Elem()
		for idx := 0; idx < arrValue.Len(); idx++ {
			dataValue.Set(reflect.Append(dataValue, arrValue.Index(idx)))
		}
		stream.data = data
	default:
		panic("data provides to Stream must be Array or Slice")
	}
	return stream
}

// implement of Stream

func (b *baseStage) Filter(filter FilterFunc) Stream {
	return wrapSink(b, opFilter, filter)
}

func (b *baseStage) Map(mapper MapFunc) Stream {
	return wrapSink(b, opMapper, mapper)
}

func (b *baseStage) FlatMap(mapper FlatMapFunc) Stream {
	return wrapSink(b, opFlatMapper, mapper)
}

func (b *baseStage) Distinct() Stream {
	return wrapSink(b, opDistincter)
}

func (b *baseStage) DistinctByFunc(fn DistinctFunc) Stream {
	return wrapSink(b, opFuncDistincter, fn)
}

func (b *baseStage) Skip(n int) Stream {
	return wrapSink(b, opSkipper, n)
}

func (b *baseStage) Limit(n int) Stream {
	return wrapSink(b, opLimiter, n)
}

func (b *baseStage) Sort(comparator ComparatorFunc) Stream {
	return wrapSink(b, opSorter, comparator)
}

func (b *baseStage) Group(grouper GroupFunc) Stream {
	return wrapSink(b, OpGrouper, grouper)
}

func (b *baseStage) Parallel() Stream {
	return wrapSink(b, OpParalleled)
}

func (b *baseStage) Max(comparator ComparatorFunc) interface{} {
	downStream := wrapSink(b, opMaximizer, comparator)
	b.startStage.end()
	return downStream.(*maxOp).max
}

func (b *baseStage) Min(comparator ComparatorFunc) interface{} {
	downStream := wrapSink(b, opMinimizer, comparator)
	b.startStage.end()
	return downStream.(*minOp).min
}

func (b *baseStage) ForEach(forEach ForEachFunc) {
	wrapSink(b, opLooper, forEach)
	b.startStage.end()
	return
}

func (b *baseStage) Collect() []interface{} {
	downStream := wrapSink(b, opCollector)
	b.startStage.end()
	return downStream.(*collectOp).data
}

func (b *baseStage) Count() int {
	downStream := wrapSink(b, opCounter)
	b.startStage.end()
	return downStream.(*countOp).count
}

func (b *baseStage) First() interface{} {
	downStream := wrapSink(b, opFirst)
	b.startStage.end()
	return downStream.(*firstOp).val
}

func (b *baseStage) Last() interface{} {
	downStream := wrapSink(b, opLast)
	b.startStage.end()
	return downStream.(*lastOp).val
}

func (b *baseStage) Reduce(reduce ReduceFunc, out interface{}) error {
	downStream := wrapSink(b, opReduce, reduce, out)
	b.startStage.end()
	return downStream.(*reduceOp).err
}

// implement sink
func (b *baseStage) begin(size int) {
	if b.downStream != nil {
		b.downStream.begin(size)
	}
}

func (b *baseStage) end() {
	if b.downStream != nil {
		b.downStream.end()
	}
}

func (b *baseStage) accept(t interface{}) {
	if b.downStream != nil {
		b.downStream.accept(t)
	}
}

func (b *baseStage) cancellationRequested() bool {
	return false
}

// implement of stage
func (b *baseStage) getStartStage() *startOp {
	return b.startStage
}

func (b *baseStage) getNextSink() sink {
	return b.downStream
}

func (b *baseStage) setStartStage(s *startOp) {
	b.startStage = s
}

func (b *baseStage) setNextSink(s sink) {
	b.downStream = s
}

// startOp presents the beginning of a stream
type startOp struct {
	baseStage
	data   []interface{}
	closed bool
}

func (s *startOp) end() {
	if s.closed {
		panic("stream already closed")
	}
	s.downStream.begin(len(s.data))
	for idx := range s.data {
		s.downStream.accept(s.data[idx])
		if s.downStream.cancellationRequested() {
			break
		}
	}
	s.downStream.end()
}
