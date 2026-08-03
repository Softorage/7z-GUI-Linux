package domain

// OperationLog represents an individual execution record in the operations history.
type OperationLog struct {
	ID        int
	File      string
	OpType    string // e.g., "Extracting", "Compressing"
	Status    string // e.g., "In Progress", "Completed", "Failed"
	Timestamp string
}