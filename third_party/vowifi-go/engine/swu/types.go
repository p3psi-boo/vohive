package swu

type DataplaneMode string

const (
	DataplaneModeKernel    DataplaneMode = "kernel"
	DataplaneModeUserspace DataplaneMode = "userspace"
)
