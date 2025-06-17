package design

import (
	g "goa.design/goa/v3/dsl"
)

var Identifier = g.Type("Identifier", g.String, func() {
	g.Description("A user-specified identifier. Must be 1-63 characters, contain only lower-cased letters and hyphens, start and end with a letter or number, and not contain consecutive hyphens.")
	// Intentionally not using a pattern here for two reasons:
	// - Go regex doesn't support lookahead, so we can't express the consecutive
	//   hyphen rule.
	// - The pattern is somewhat complex, so the error message is hard to
	//   interpret when the value doesn't match.
	g.MinLength(1)
	g.MaxLength(63)
	g.Example("production")
	g.Example("my-app")
	g.Example("76f9b8c0-4958-11f0-a489-3bb29577c696")
})
