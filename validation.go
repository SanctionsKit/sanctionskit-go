package sanctionskit

import (
	"strings"
	"time"
	"unicode/utf8"
)

func textLength(value string, min, max int) bool {
	n := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && n >= min && n <= max
}

func validateRequest(r ScreeningRequest) error {
	s := r.Subject
	if !textLength(strings.TrimSpace(s.Name), 2, 300) {
		return invalidInput()
	}
	switch s.EntityType {
	case "", "person", "organization", "vessel", "aircraft", "other":
	default:
		return invalidInput()
	}
	if s.Country != "" && !textLength(s.Country, 2, 100) {
		return invalidInput()
	}
	if s.BirthDate != "" {
		layout := ""
		switch len(s.BirthDate) {
		case 4:
			layout = "2006"
		case 7:
			layout = "2006-01"
		case 10:
			layout = "2006-01-02"
		default:
			return invalidInput()
		}
		parsed, err := time.Parse(layout, s.BirthDate)
		if err != nil || parsed.Year() < 1 || parsed.Format(layout) != s.BirthDate {
			return invalidInput()
		}
	}
	if len(s.Identifiers) > 20 {
		return invalidInput()
	}
	for _, i := range s.Identifiers {
		if !textLength(i.Type, 1, 80) || !textLength(i.Value, 1, 160) || (i.Issuer != "" && !textLength(i.Issuer, 1, 100)) {
			return invalidInput()
		}
	}
	if (r.Package == "") == (len(r.Sources) == 0) {
		return invalidInput()
	}
	if r.Package != "" && !textLength(r.Package, 1, 80) {
		return invalidInput()
	}
	if len(r.Sources) > 512 {
		return invalidInput()
	}
	for _, s := range r.Sources {
		if !textLength(s, 1, 80) {
			return invalidInput()
		}
	}
	if !textLength(r.Reference, 0, 160) {
		return invalidInput()
	}
	if r.CounterpartyID != "" && !uuidPattern.MatchString(r.CounterpartyID) {
		return invalidInput()
	}
	if r.Policy != nil && (!uuidPattern.MatchString(r.Policy.ID) || r.Policy.Version < 1 || r.Policy.Version > 9007199254740991) {
		return invalidInput()
	}
	if r.Retention != "" && r.Retention != "standard" && r.Retention != "minimal" {
		return invalidInput()
	}
	return nil
}
