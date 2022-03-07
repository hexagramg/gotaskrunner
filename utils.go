package gotaskrunner

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type safeActiveMap struct {
	lock   sync.RWMutex
	values map[uuid.UUID]*struct {
		task   *TaskWrapper
		thread int
	}
}

func NewSaveActiveMap() *safeActiveMap {
	return &safeActiveMap{
		values: make(map[uuid.UUID]*struct {
			task   *TaskWrapper
			thread int
		}),
	}
}

func (s *safeActiveMap) Set(task *TaskWrapper, thread int) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.values[task.id] = &struct {
		task   *TaskWrapper
		thread int
	}{task, thread}
}

func (s *safeActiveMap) UpdateThread(id uuid.UUID, thread int) (ok bool) {

	s.lock.Lock()
	defer s.lock.Unlock()
	if v, ok := s.values[id]; ok {
		v.thread = thread
	}
	return ok

}

func (s *safeActiveMap) Len() int {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.values)
}

func (s *safeActiveMap) Get(id uuid.UUID) (wrapper *TaskWrapper, thread int, ok bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if r, ok := s.values[id]; ok {
		if r != nil {
			wrapper = r.task
			thread = r.thread
		}
		ok = true
	}
	return
}

func (s *safeActiveMap) Delete(id uuid.UUID) (ok bool) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok = s.values[id]; ok { //just to know if anything was actually deleted
		delete(s.values, id)
	}
	return
}

type safeInt struct {
	lock  sync.RWMutex
	value int
}

func (s *safeInt) Set(val int) *safeInt {
	s.lock.Lock()
	s.value = val
	s.lock.Unlock()
	return s
}

func (s *safeInt) Inc() *safeInt {
	s.lock.Lock()
	s.value++
	s.lock.Unlock()
	return s
}

func (s *safeInt) Dec() *safeInt {
	s.lock.Lock()
	s.value--
	s.lock.Unlock()
	return s
}

func (s *safeInt) Get() int {
	s.lock.RLock()
	val := s.value
	s.lock.RUnlock()
	return val
}

type PlanBuilder struct {
	repeat time.Duration
	limit  int
	offset time.Duration
	exc    []ExceptDate
	shift  time.Duration
}

//Alternative way of creating plan sting
func NewPlanBuilder() *PlanBuilder {
	return &PlanBuilder{}
}

func (b *PlanBuilder) SetRepeat(repeat time.Duration) *PlanBuilder {
	b.repeat = repeat
	return b
}

func (b *PlanBuilder) SetLimit(limit int) *PlanBuilder {
	b.limit = limit
	return b
}

func (b *PlanBuilder) SetOffset(offset time.Duration) *PlanBuilder {
	b.offset = offset
	return b
}

func (b *PlanBuilder) SetExceptDates(exc ...ExceptDate) *PlanBuilder {
	b.exc = exc
	return b
}

func (b *PlanBuilder) SetShift(shift time.Duration) *PlanBuilder {
	b.shift = shift
	return b
}

func (b *PlanBuilder) String() string {
	var plan []string = make([]string, 0, 5)
	if b.repeat.Seconds() != 0 {
		plan = append(plan, "r"+b.repeat.String())
	}
	if b.limit != 0 {
		plan = append(plan, "l"+strconv.Itoa(b.limit))
	}
	if b.offset.Seconds() != 0 {
		plan = append(plan, "o"+b.offset.String())
	}
	if len(b.exc) > 0 {
		buffer := make([]string, 0, len(b.exc))
		for _, v := range b.exc {
			buffer = append(buffer, strconv.Itoa(int(v)))
		}
		plan = append(plan, "e"+strings.Join(buffer, ","))
	}
	if b.shift.Seconds() != 0 {
		plan = append(plan, "s"+b.shift.String())
	}
	return strings.Join(plan, ":")
}
