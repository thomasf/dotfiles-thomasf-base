package main

// ListItem is an entry of op item list --format=json.
type ListItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Vault    Vault  `json:"vault"`
}

// Item is the output of op item get --format=json. Field values, secrets
// included, are present without --reveal.
type Item struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Category string  `json:"category"`
	Vault    Vault   `json:"vault"`
	Fields   []Field `json:"fields"`
	URLs     []URL   `json:"urls"`
}

type Vault struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Field is an item field. Value is absent from the JSON for an empty field.
type Field struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`    // STRING, CONCEALED, OTP, SSHKEY, URL, EMAIL, DATE, ...
	Purpose string   `json:"purpose"` // USERNAME, PASSWORD, NOTES or empty
	Label   string   `json:"label"`
	Section *Section `json:"section"`
	Value   string   `json:"value"`
	TOTP    string   `json:"totp"` // current code of an OTP field
}

type Section struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type URL struct {
	Label   string `json:"label"`
	Primary bool   `json:"primary"`
	Href    string `json:"href"`
}
