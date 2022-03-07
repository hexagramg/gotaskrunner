package gotaskrunner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Client struct {
	name               string
	ctx                context.Context
	cancel             context.CancelFunc
	wrapper_channel    chan *TaskWrapper
	wrappers           []*TaskWrapper
	wrappers_sync      sync.RWMutex //protects wrappers slice
	ticks              time.Duration
	wrappers_failed    []*TaskWrapper
	wrappers_done      []*TaskWrapper
	wrappers_done_sync sync.RWMutex //protects wrappers_done and wrappers_failed
	logger             func(*TaskWrapper)
	working            safeInt
	active             safeInt
	thread_timeout     time.Duration
	max_threads        int
	wrappers_active    *safeActiveMap
	state              WaitState
	state_condition    sync.Cond
}

const (
	THREAD_TIMEOUT_MULTIPLYER = 10
)

type WaitState int

const (
	TASKS_FINISHED    WaitState = iota // Nothing is executing and nothing is planned
	NO_TASKS_ACTIVE                    // something is planned but nothing is executing
	LAST_TASKS_ACTIVE                  // nothing is planned but something is executing
	TASKS_ACTIVE                       // tasks are planned and executing

)

func NewRunner(ctx context.Context, polling time.Duration, name ...string) *Client {
	newctx, cancel := context.WithCancel(ctx)
	var n string
	if len(name) > 0 {
		n = name[0]
	}
	c := Client{
		ctx:             newctx,
		cancel:          cancel,
		wrappers:        make([]*TaskWrapper, 0, 100),
		wrappers_active: NewSaveActiveMap(),
		wrapper_channel: make(chan *TaskWrapper, 10),
		ticks:           polling,
		thread_timeout:  THREAD_TIMEOUT_MULTIPLYER * polling,
		name:            n,
		state_condition: *sync.NewCond(&sync.RWMutex{}),
	}
	return &c
}

//stop the runner, all tasks that are already executing will continue to do so until they are done
func (c *Client) Stop() {
	c.cancel()
	c.wrappers = nil
}

func (c *Client) update_state() {
	type client_state_bool struct {
		planned, active bool
	}
	var check client_state_bool
	c.wrappers_sync.RLock()
	check.planned = len(c.wrappers) > 0
	c.wrappers_sync.RUnlock()

	check.active = c.wrappers_active.Len() > 0
	var state WaitState
	switch check {
	case client_state_bool{true, true}:
		state = TASKS_ACTIVE
	case client_state_bool{false, true}:
		state = LAST_TASKS_ACTIVE
	case client_state_bool{true, false}:
		state = NO_TASKS_ACTIVE
	case client_state_bool{false, false}:
		state = TASKS_FINISHED
	}
	if state != c.state {
		c.state_condition.L.Lock()
		c.state = state
		c.state_condition.L.Unlock()
		c.state_condition.Broadcast()
	}
}

func (c *Client) GetState() WaitState {
	c.state_condition.L.(*sync.RWMutex).RLock()
	defer c.state_condition.L.(*sync.RWMutex).RUnlock()
	return c.state
}

func (c *Client) Wait(w WaitState) {
	state := c.GetState()
	for state != w {
		c.state_condition.L.Lock()
		c.state_condition.Wait()
		state = c.state
		c.state_condition.L.Unlock()
	}

}

func (c *Client) GetFailedWrappers() []*TaskWrapper {
	return c.wrappers_failed
}

func (c *Client) GetSuccessFulWrappers() []*TaskWrapper {
	return c.wrappers_done
}

func (c *Client) finish_wrapper(wrapper *TaskWrapper) {

	wrapper.clean()
	ok := c.wrappers_active.Delete(wrapper.id) // if not ok then it was already deleted
	if ok {                                    // if wrapper was in active, then this function runs first time
		c.wrappers_done_sync.Lock()
		if wrapper.Failed() {
			c.wrappers_failed = append(c.wrappers_failed, wrapper)
		} else {
			c.wrappers_done = append(c.wrappers_done, wrapper)
		}
		c.wrappers_done_sync.Unlock()
		c.update_state()
	}
}

const OPTGOROUTINES = 10

var InitialTimeout time.Duration = time.Minute

//runner threads
func (c *Client) run_thread(num int, input <-chan *TaskWrapper) {
	var wrapper *TaskWrapper
	defer func() {
		if r := recover(); r != nil {
			log.Println("PANIC in run_thread ", num, " of ", r)
			if wrapper != nil {
				c.finish_wrapper(wrapper)
			}

		}
	}()
	c.active.Inc()
	defer c.active.Dec()

	timer := time.NewTimer(InitialTimeout)
	var working_set bool
	defer func() {
		if working_set {
			c.working.Dec()
		}
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-timer.C:
			return
		case wrapper = <-input:
			working_set = true
			c.working.Inc()
			c.wrappers_active.UpdateThread(wrapper.id, num)
			wrapper.execute()
			if c.logger != nil {
				c.logger(wrapper)
			}
			c.finish_wrapper(wrapper)
		}
		working_set = false
		c.working.Dec()
		timer = time.NewTimer(c.thread_timeout)
	}
}

var IterTimer time.Duration = time.Hour

//polling launching of tasks when they are ready
func (c *Client) feeder_thread(output chan<- *TaskWrapper) {
	ticker := time.NewTicker(c.ticks)
	timer := time.NewTimer(IterTimer)
	for {
		select {
		case <-timer.C:
			log.Printf("RUNNER %T, name:%v, active:%v, working:%v", c, c.name, c.active.Get(), c.working.Get())
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.working.Get() < c.max_threads {
				now := time.Now()
				if len(c.wrappers) > 0 {
					// how many wrappers(tasks) are due to execute
					var to_extract int
					c.wrappers_sync.RLock()
					for i, p := range c.wrappers {
						planned_time := p.GetTime()
						if planned_time.After(now) {
							break
						} else {
							to_extract = i + 1
						}
					}
					c.wrappers_sync.RUnlock()

					if to_extract > 0 {
						//remove those wrappers from planned slice
						c.wrappers_sync.Lock()
						extracted := c.wrappers[0:to_extract]
						if to_extract < len(c.wrappers) {
							c.wrappers = c.wrappers[to_extract:]
						} else {
							c.wrappers = make([]*TaskWrapper, 0, 10)
						}
						c.wrappers_sync.Unlock()

						//launch threads if it is needed
						if act := c.active.Get(); act < c.max_threads {
							for b := 1; b <= c.max_threads-act && b <= to_extract; b++ {
								go c.run_thread(act+b, c.wrapper_channel)
							}
						}
						//send wrappers
						for _, v := range extracted {
							c.wrappers_active.Set(v, -1)
							output <- v
						}
					}
				}
			} else if w, a := c.working.Get(), c.active.Get(); w > a || w < 0 || a < 0 || w > c.max_threads || a > c.max_threads {

				panic(fmt.Sprintf("more working than active somehow or negative w:%v, a:%v, max:%v", w, a, c.max_threads))
			}

		}
		c.update_state()
	}
}

// start the runner. threads -- maximum working goroutines
func (c *Client) Run(threads int) {
	if threads < 1 {
		threads = OPTGOROUTINES
	}
	c.max_threads = threads
	go c.feeder_thread(c.wrapper_channel)
	c.update_state()
}

var ErrNotAPointer = errors.New("underlying value of task interface should be a pointer")

func (c *Client) plan(w *TaskWrapper) {
	c.wrappers_sync.Lock()
	pos := c.search(w.GetTime())
	c.insert(pos, w)
	c.wrappers_sync.Unlock()
}

//get wrapper for the passed task
func (c *Client) SetTask(t Task) *TaskWrapper {
	if reflect.ValueOf(t).Kind() != reflect.Ptr {
		panic(ErrNotAPointer)
	}
	w := TaskWrapper{
		id:     uuid.New(),
		client: c,
		task:   t,
	}
	return &w
}

//get planned wrappers, do not edit this slice
func (c *Client) GetWrappers() []*TaskWrapper {
	return c.wrappers
}

//I will do plain search now, will do something more fast later if needed
func (c *Client) search(planned time.Time) int {
	if len(c.wrappers) == 0 {
		return 0
	}
	if c.wrappers[0].GetTime().After(planned) {
		return 0
	}
	if c.wrappers[len(c.wrappers)-1].GetTime().Before(planned) {
		return len(c.wrappers)
	}
	var l, r int = 0, len(c.wrappers) - 1
	for l < r {
		var mid int = l + (r-l)/2
		t := c.wrappers[mid].GetTime()
		if planned.Before(t) {
			r = mid - 1
		} else {
			l = mid + 1
		}
	}
	return l
}

func (c *Client) insert(pos int, w *TaskWrapper) {
	// do not lock mutex here
	if pos == 0 {
		c.wrappers = append([]*TaskWrapper{w}, c.wrappers...)
	} else if pos < len(c.wrappers) {
		c.wrappers = append(c.wrappers[:pos+1], c.wrappers[pos:]...)
		c.wrappers[pos] = w
	} else {
		c.wrappers = append(c.wrappers, w)
	}
}

// get currently successful and failed wrappers and assign new internal slices for them
func (c *Client) CleanCompletedWrappers() (successful []*TaskWrapper, failed []*TaskWrapper) {
	c.wrappers_done_sync.Lock()
	defer c.wrappers_done_sync.Unlock()
	successful = c.GetSuccessFulWrappers()
	failed = c.GetFailedWrappers()
	c.wrappers_done = make([]*TaskWrapper, 0, 5)
	c.wrappers_failed = make([]*TaskWrapper, 0, 5)
	return

}

//attach logger, executed after every task completion
func (c *Client) AttachLoggerFunction(logger func(wrapper *TaskWrapper)) *Client {
	c.logger = logger
	return c
}
