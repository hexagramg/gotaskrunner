package gotaskrunner

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type sampleIncTask struct {
	inc *safeInt
}

func (s *sampleIncTask) Run() error {
	s.inc.Inc()
	return nil
}

type errorTask struct{}

func (e *errorTask) Run() error {
	return fmt.Errorf("")
}

type panicTask struct{}

func (p *panicTask) Run() error {
	panic(fmt.Errorf("panic"))
}

func TestSequential(t *testing.T) {
	var s safeInt
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	client := NewRunner(ctx, time.Millisecond)
	for i := 0; i < 10; i++ {
		task := &sampleIncTask{&s}
		client.SetTask(task).PlanForExecution()
	}
	client.Run(1)
	client.Wait(TASKS_FINISHED)
	if s.Get() != 10 {
		t.Fail()
	}
}

func TestCallbacks(t *testing.T) {

	var s safeInt
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	client := NewRunner(ctx, time.Millisecond)
	task := &sampleIncTask{&s}
	var ctasks []*TaskWrapper = make([]*TaskWrapper, 0, 10)
	for i := 0; i < 10; i++ {
		ctasks = append(ctasks, client.SetTask(&sampleIncTask{&s}))
	}
	client.SetTask(task).SetNextWrappers(ctasks...).PlanForExecution()
	client.Run(3)
	client.Wait(TASKS_FINISHED)
	if s.Get() != 11 {
		t.Fail()
	}
}

func TestErrors(t *testing.T) {

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	runner := NewRunner(ctx, time.Millisecond)
	tasks := []Task{&errorTask{}, &panicTask{}}

	for _, v := range tasks {
		runner.SetTask(v).PlanForExecution()
	}

	runner.Run(5)
	runner.Wait(TASKS_FINISHED)
	if len(runner.GetFailedWrappers()) != 2 {
		t.Fail()
	}
}

func TestPlanString(t *testing.T) {

	var s safeInt
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	runner := NewRunner(ctx, time.Millisecond)
	runner.Run(2)
	task := &sampleIncTask{&s}
	//test limit
	wr := runner.SetTask(task).SetPlanWithString("l2")
	if wr.limit != 1 {
		t.Fail()
	}
	wr.PlanForExecution()
	runner.Wait(TASKS_FINISHED)
	if s.Get() != 2 {
		t.Fail()
	}
	//test repeat
	var repeat = 5 * time.Millisecond
	wr = runner.SetTask(task).SetPlanWithString(fmt.Sprintf("r%v:l2:o%v", repeat, repeat))
	if wr.repeat != repeat {
		t.Fail()
	}
	now := time.Now()
	wr.PlanForExecution()
	runner.Wait(TASKS_FINISHED)
	delta := time.Since(now)
	if delta < 2*repeat {
		t.Fail()
	}

}

func TestPlanBuilder(t *testing.T) {
	temp := time.Millisecond
	plan := fmt.Sprintf("r%v:l5:o%v:e%v,%v:s%v", temp, 2*temp, Monday, Tuesday, 3*temp)

	builder := NewPlanBuilder().SetLimit(5).SetRepeat(temp).SetOffset(2*temp).SetShift(3*temp).SetExceptDates(Monday, Tuesday)

	if bs := builder.String(); plan != bs {
		t.Fail()
	}

	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	runner := NewRunner(ctx, time.Millisecond)
	t1, t2 := &sampleIncTask{}, &sampleIncTask{}
	wr1, wr2 := runner.SetTask(t1), runner.SetTask(t2)
	wr1.SetPlanWithBuilder(builder)
	wr2.SetPlanWithString(plan)

	if wr1.limit != wr2.limit || wr1.offset != wr2.offset || wr1.repeat != wr2.repeat || wr1.ex_shift != wr2.ex_shift || len(wr1.except) != len(wr2.except) {
		t.Fail()
	}

}

func TestSearch(t *testing.T) {

	var s safeInt
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	runner := NewRunner(ctx, time.Millisecond)
	task := &sampleIncTask{&s}

	wr1, wr2, wr3, wr4 := runner.SetTask(task), runner.SetTask(task), runner.SetTask(task), runner.SetTask(task)
	wr2.SetPlanWithString(fmt.Sprintf("o%v", 5*time.Millisecond)).PlanForExecution()
	wr1.PlanForExecution()
	wr3.SetPlanWithString(fmt.Sprintf("o%v", 2*time.Millisecond)).PlanForExecution()
	wr4.SetPlanWithString(fmt.Sprintf("o%v", 6*time.Millisecond)).PlanForExecution()
	if len(runner.wrappers) != 4 {
		t.Fail()
	}
	if runner.wrappers[0].id != wr1.id || runner.wrappers[3].id != wr4.id {
		t.Fail()
	}
	runner.Run(2)

	runner.Wait(TASKS_FINISHED)

	d, f := runner.CleanCompletedWrappers()
	if len(f) != 0 || len(d) != 4 {
		t.Fail()
	}
}
