package gotaskrunner

type Task interface {
	Run() error
}

type TaskDescriptor interface {
	Description() string
}

type TaskSetter interface {
	//If callback task needs some data from the completed task implement this method
	SetData(Task)
}

type TaskCleaner interface {
	// Clean large chunks of data inside after completion
	Clean()
}

type TaskCopy interface {
	Copy() Task
}

type Manager interface {
	GetWrappers() []*TaskWrapper
	GetFailedWrappers() []*TaskWrapper
	GetSuccessFulWrappers() []*TaskWrapper
	CleanCompletedWrappers() (successful []*TaskWrapper, failed []*TaskWrapper)
	CreateWrapper(Task) *TaskWrapper
	AttachLoggerFunction(func(*TaskWrapper))
	Stop()
	Run(int)
}
