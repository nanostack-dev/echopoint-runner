package edge

type Type string

const (
	TypeDefault Type = "normal"
	TypeSuccess Type = "success"
	TypeFailure Type = "failure"
)

type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Type   Type   `json:"type"`
}

func (e Edge) IsDefault() bool {
	return e.Type == TypeDefault
}

func (e Edge) IsSuccess() bool {
	return e.Type == TypeSuccess
}

func (e Edge) IsFailure() bool {
	return e.Type == TypeFailure
}
