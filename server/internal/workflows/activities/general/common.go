package general

import "fmt"

type Owner struct {
	User  string `json:"user,omitempty"`
	Group string `json:"group,omitempty"`
}

func (o *Owner) String() string {
	return fmt.Sprintf("%s:%s", o.User, o.Group)
}
