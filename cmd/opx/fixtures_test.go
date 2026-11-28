package main

import _ "embed"

// Output of op item get --format=json, with fake values.
var (
	//go:embed testdata/item-login.json
	itemLoginJSON []byte
	//go:embed testdata/item-password.json
	itemPasswordJSON []byte
	//go:embed testdata/item-api-credential.json
	itemAPICredentialJSON []byte
	//go:embed testdata/item-ssh-key.json
	itemSSHKeyJSON []byte
	//go:embed testdata/item-identity.json
	itemIdentityJSON []byte
)
