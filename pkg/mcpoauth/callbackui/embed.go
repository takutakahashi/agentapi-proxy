package callbackui

import _ "embed"

// CallbackHTML is the HTML template for the OAuth callback result page.
//
//go:embed callback.html
var CallbackHTML string
