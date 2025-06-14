package design

import (
	g "goa.design/goa/v3/dsl"
)

// Allows IDs that are:
// - 1-63 characters
// - Contain only lower-cased letters and hyphens
// - Starts with a letter or number
// - Ends with a letter or number
// The handlers must also validate that there are no consecutive hyphens.
const idPattern = `^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`

var Identifier = g.Type("Identifier", g.String, func() {
	g.Description("A user-specified identifier. Must be 1-63 characters, contain only lower-cased letters and hyphens, start and end with a letter or number, and not contain consecutive hyphens.")
	g.Pattern(idPattern)
	g.Example("production")
	g.Example("my-app")
	g.Example("76f9b8c0-4958-11f0-a489-3bb29577c696")
})
