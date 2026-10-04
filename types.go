package sanctionskit

import "encoding/json"

// Subject contains known identity information. BirthDate preserves the supplied
// precision: YYYY, YYYY-MM, or YYYY-MM-DD. Omit unknown optional fields.
type Subject struct {
	Name        string       `json:"name"`
	EntityType  string       `json:"entityType,omitempty"`
	Identifiers []Identifier `json:"identifiers,omitempty"`
	BirthDate   string       `json:"birthDate,omitempty"`
	Country     string       `json:"country,omitempty"`
}

// Identifier is an original identifier and its scheme, with an optional issuer.
type Identifier struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Issuer string `json:"issuer,omitempty"`
}

// PolicyReference identifies one exact approved organization policy version.
type PolicyReference struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

// ScreeningRequest selects exactly one of Package or Sources. Policies and
// organization controls may require standard retention or restrict coverage.
type ScreeningRequest struct {
	Subject        Subject          `json:"subject"`
	CounterpartyID string           `json:"counterpartyId,omitempty"`
	Policy         *PolicyReference `json:"policy,omitempty"`
	Reference      string           `json:"reference,omitempty"`
	Sources        []string         `json:"sources,omitempty"`
	Package        string           `json:"package,omitempty"`
	Retention      string           `json:"retention,omitempty"`
}

// Versions identifies the data and matching rules used for a result.
type Versions struct {
	Dataset        string `json:"dataset"`
	MatchingEngine string `json:"matchingEngine"`
	Policy         string `json:"policy"`
	Package        string `json:"package,omitempty"`
}

// Coverage preserves exact source versions and freshness. The complete original
// object, including additional publisher notices, remains in Response.RawJSON.
type Coverage struct {
	SourceID    string `json:"sourceId"`
	Version     string `json:"version"`
	RetrievedAt string `json:"retrievedAt"`
	Fresh       bool   `json:"fresh"`
	// PublishedAt can be a date or a timestamp. Preserve its original precision.
	PublishedAt  *string         `json:"publishedAt,omitempty"`
	SourceNotice json.RawMessage `json:"sourceNotice,omitempty"`
}

// Match contains a candidate and its supporting evidence. Raw nested objects
// preserve varying publisher fields without inventing a common record schema.
type Match struct {
	Score      float64         `json:"score"`
	Record     json.RawMessage `json:"record"`
	Evidence   json.RawMessage `json:"evidence"`
	Conflicts  []string        `json:"conflicts"`
	Assessment json.RawMessage `json:"assessment,omitempty"`
}

// ScreeningResult is the original screening outcome. Status is potential_match
// or no_match. Neither status is an approval decision.
type ScreeningResult struct {
	ID             string          `json:"id"`
	Environment    string          `json:"environment"`
	Status         string          `json:"status"`
	CreatedAt      string          `json:"createdAt"`
	Matches        []Match         `json:"matches"`
	Coverage       []Coverage      `json:"coverage"`
	Versions       Versions        `json:"versions"`
	Disclaimer     string          `json:"disclaimer"`
	PolicySnapshot json.RawMessage `json:"policySnapshot,omitempty"`
	Matching       json.RawMessage `json:"matching,omitempty"`
}

// RetainedResult adds retained inputs. Subject and Reference are nil when absent
// under the retention policy. They are not reconstructed by the client.
type RetainedResult struct {
	ScreeningResult
	Subject   *Subject `json:"subject"`
	Reference *string  `json:"reference"`
}

// Evidence is the raw evidence document's typed view. Nullable fields remain
// nil. The document is not a signed attestation or compliance certification.
type Evidence struct {
	Format         string            `json:"format"`
	Result         ScreeningResult   `json:"result"`
	Retention      string            `json:"retention"`
	ExpiresAt      *string           `json:"expires_at"`
	Subject        *Subject          `json:"subject"`
	Reference      *string           `json:"reference"`
	Request        *ScreeningRequest `json:"request"`
	RetainedInputs bool              `json:"retainedInputs"`
	ReplayLimit    *string           `json:"replayLimit"`
}

// Source describes current availability and supported entity types. Presence in
// the catalog alone does not mean a source can be used for a screening.
type Source struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Authority      string          `json:"authority"`
	Availability   string          `json:"availability"`
	Capabilities   []string        `json:"capabilities"`
	RightsStatus   string          `json:"rightsStatus"`
	Category       string          `json:"category"`
	Jurisdiction   string          `json:"jurisdiction,omitempty"`
	Homepage       string          `json:"homepage,omitempty"`
	DisabledReason string          `json:"disabledReason,omitempty"`
	Fresh          *bool           `json:"fresh,omitempty"`
	Qualification  json.RawMessage `json:"qualification,omitempty"`
	CurrentVersion json.RawMessage `json:"currentVersion,omitempty"`
	LastImport     json.RawMessage `json:"lastImport,omitempty"`
}

// Response contains parsed data, a correlation ID when supplied, and the exact
// original JSON response bytes. Keep RawJSON in access-controlled storage.
type Response[T any] struct {
	Data      T
	RequestID string
	RawJSON   json.RawMessage
}

// EvidenceResponse keeps the direct evidence document separate from ordinary
// API data envelopes. RawJSON contains the unchanged downloaded document.
type EvidenceResponse struct {
	Evidence  Evidence
	RequestID string
	RawJSON   json.RawMessage
}
