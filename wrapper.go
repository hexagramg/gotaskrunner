package gotaskrunner

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type TaskWrapper struct {
	id           uuid.UUID
	client       *Client
	task         Task
	plan         string
	callbacks    []*TaskWrapper
	planned_time time.Time
	not_first    bool
	limit        int
	repeat       time.Duration
	offset       time.Duration
	except       []ExceptDate
	ex_shift     time.Duration
	wait_lock    sync.Mutex
	job_started  time.Time
	job_ended    time.Time
	job_error    error
	job_panic    interface{}
}

func (gw *TaskWrapper) GetTask() Task {
	return gw.task
}

type ExceptDate int

//you can except certain weekdays from completion
const (
	Sunday ExceptDate = iota
	Monday
	Tuesday
	Wendsday
	Thursday
	Friday
	Saturday
)

func (gw *TaskWrapper) SetPlanWithBuilder(builder *PlanBuilder) *TaskWrapper {

	if builder.repeat.Seconds() != 0 {
		gw.repeat = builder.repeat
	}
	if builder.limit != 0 {
		gw.limit = builder.limit - 1
	}
	if builder.offset.Seconds() != 0 {
		gw.offset = builder.offset
	}
	if len(builder.exc) > 0 {
		gw.except = builder.exc
	}
	if builder.shift.Seconds() != 0 {
		gw.ex_shift = builder.shift
	}
	gw.plan = builder.String()
	return gw
}

var ErrPlanErr = errors.New("plan has wrong formatting")
var ErrPlanLoop = errors.New("plan is uncompatible")

/* plan is a string that is separated by ':',  the first letter is a mode
empty plan means immideate plan and zero repeat
r - repeat every duration
l - limit how many times job is done, -1 for infinite
o - offset duration
e - except Exceptdate separated by commas
s - how much to shift time of execution when ExceptDate encountered, if not supplied it is shifted by 'r'
*/
func (gw *TaskWrapper) SetPlanWithString(plan string) *TaskWrapper {
	gw.plan = plan
	if len(gw.plan) > 0 {

		if gw.except == nil {
			gw.except = make([]ExceptDate, 0, 3)
		}
		var sep []string = strings.Split(gw.plan, ":")
		for i, word := range sep {
			if len(word) < 2 {
				panic(ErrPlanErr)
			}
			body := word[1:]
			switch s := sep[i]; s[0] {
			case 'r':
				dur, err := time.ParseDuration(body)
				if err != nil {
					panic(err)
				}
				gw.repeat = dur
			case 'o':
				dur, err := time.ParseDuration(body)
				if err != nil {
					panic(err)
				}
				gw.offset = dur
			case 's':
				dur, err := time.ParseDuration(body)
				if err != nil {
					panic(err)
				}
				gw.ex_shift = dur
			case 'l':
				v, err := strconv.Atoi(body)
				if err != nil {
					panic(err)
				}
				gw.limit = v - 1
			case 'e':
				exceptions := strings.Split(body, ",")
				for i := range exceptions {
					if len(exceptions[i]) > 0 {
						v, err := strconv.Atoi(exceptions[i])
						if err != nil {
							panic(err)
						}
						gw.except = append(gw.except, ExceptDate(v))
					}
				}
			default:
				panic(ErrPlanErr)
			}
		}
	}
	return gw
}

// Clone a wrapper, task is the same if it does not implement TaskCopy interface
func (gw *TaskWrapper) Clone() *TaskWrapper {
	new_callbacks := make([]*TaskWrapper, 0, len(gw.callbacks))
	for i := range gw.callbacks {
		wr := gw.callbacks[i].Clone()
		new_callbacks = append(new_callbacks, wr)
	}
	var task Task
	if copy, ok := gw.task.(TaskCopy); ok {
		task = copy.Copy()
	} else {
		task = gw.task
	}
	nw := TaskWrapper{
		id:        uuid.New(),
		client:    gw.client,
		task:      task,
		plan:      gw.plan,
		callbacks: new_callbacks,
		limit:     gw.limit,
		repeat:    gw.repeat,
		offset:    gw.offset,
		except:    gw.except,
		ex_shift:  gw.ex_shift,
		not_first: gw.not_first,
	}
	return &nw
}

// Get planned time
func (gw *TaskWrapper) GetTime() time.Time {
	return gw.planned_time
}

func (gw *TaskWrapper) check_ex(t time.Time) bool {

	wd := int(t.Weekday())
	skip := false
	for i := range gw.except {
		if wd == int(gw.except[i]) {
			skip = true
			break
		}
	}
	return skip
}

var ErrAlreadyPlanned = errors.New("this wrapper is already planned for execution")

//  Plan the task for execution
func (gw *TaskWrapper) PlanForExecution() *TaskWrapper {
	if !gw.planned_time.IsZero() {
		panic(ErrAlreadyPlanned)
	}
	if gw.limit < 0 && gw.limit > -2 {
		return gw
	}
	planned := time.Now()
	if len(gw.plan) > 0 {

		if gw.offset.Seconds() != 0 {
			planned = planned.Add(gw.offset)
			gw.offset = 0 * time.Second
		} else if gw.not_first {
			planned = planned.Add(gw.repeat)
		}
		if !gw.not_first {
			gw.not_first = true
		}
		skip := gw.check_ex(planned)
		if skip {

			shift := true //if shift is empty need to use repeat value
			if gw.ex_shift.Seconds() == 0 {
				shift = false
			}
			if !shift && gw.repeat.Seconds() == 0 {
				panic(ErrPlanLoop)
			}
			for skip {
				if shift {
					planned = planned.Add(gw.ex_shift)
				} else {
					planned = planned.Add(gw.repeat)
				}
				skip = gw.check_ex(planned)
			}
		}
	}
	gw.planned_time = planned
	gw.limit -= 1
	gw.client.plan(gw)
	gw.wait_lock.Lock()
	gw.client.update_state()
	return gw
}

func (gw *TaskWrapper) Description() string {
	var desc string = "Descriptor not implemented"
	if d, ok := gw.task.(TaskDescriptor); ok {
		desc = d.Description()
	}
	return fmt.Sprintf("Wrapped %T TASK %v, started: %v, ended: %v, failed: %v, plan: %v",
		gw.task, desc, gw.job_started, gw.job_ended, gw.Failed(), gw.plan)
}

// set these wrappers to be planned after SUCCESSFUL completion of the task
func (gw *TaskWrapper) SetNextWrappers(wrappers ...*TaskWrapper) *TaskWrapper {
	gw.callbacks = wrappers
	return gw
}

// main execution function for wrapper, sets time and plans callbacks
func (gw *TaskWrapper) execute() {
	defer func() {
		if r := recover(); r != nil {
			gw.job_panic = r
			gw.job_ended = time.Now()
			log.Println("PANIC on TASK ", gw.Description(), " error: ", r)
		}
	}()
	defer gw.wait_lock.Unlock()
	gw.job_started = time.Now()
	err := gw.task.Run()
	gw.job_ended = time.Now()
	if err == nil {
		if len(gw.callbacks) > 0 {
			for _, v := range gw.callbacks {
				task := v.GetTask()
				if setter, ok := task.(TaskSetter); ok {
					setter.SetData(gw.GetTask())
				}
				v.PlanForExecution()
			}
		}
		gw.Clone().PlanForExecution()
	} else {
		// TODO write custom logger
		gw.job_error = err
		log.Println("ERROR on TASK ", gw.Description(), " error: ", err)
	}
}

func (gw *TaskWrapper) Failed() bool {
	return gw.job_error != nil || gw.job_panic != nil
}

func (gw *TaskWrapper) clean() {
	gw.callbacks = nil
	if nw, ok := gw.task.(TaskCleaner); ok {
		nw.Clean()
	}
}

var ErrNotPlanned = errors.New("awaiting task that is not planned")

// wait for task completion
func (gw *TaskWrapper) Wait() bool {
	if gw.planned_time.IsZero() {
		panic(ErrNotPlanned)
	}
	success := true
	gw.wait_lock.Lock()
	if gw.Failed() {
		success = false
	}
	gw.wait_lock.Unlock()

	return success
}
