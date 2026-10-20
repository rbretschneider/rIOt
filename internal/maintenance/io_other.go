//go:build !linux

package maintenance

type Lock struct{}

func AcquireLock() (*Lock, error)                 { return nil, ErrUnsupported }
func AcquireLockAt(string) (*Lock, error)         { return nil, ErrUnsupported }
func (*Lock) Close() error                        { return nil }
func ReadAgentFile(string, int64) ([]byte, error) { return nil, ErrUnsupported }
func ReadInbox() (Request, error)                 { return Request{}, ErrUnsupported }
func WriteRequest(Request) error                  { return ErrUnsupported }
func ReadRecord(string) (Record, error)           { return Record{}, ErrUnsupported }
func ReadCurrent() (Record, error)                { return Record{}, ErrUnsupported }
func ReadTerminal(string) (Record, error)         { return Record{}, ErrUnsupported }
func ReadUpdateStatus() (UpdateStatus, error)     { return UpdateStatus{}, ErrUnsupported }
func WriteUpdateStatus(UpdateStatus) error        { return ErrUnsupported }
