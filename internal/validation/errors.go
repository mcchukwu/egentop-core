package validation

type Errors map[string]string

func (e Errors) HasErrors() bool {
	return len(e) > 0
}
