package client

import "regexp"

// misplacedPreProcessField matches a LoadMaster firmware bug (seen on
// 7.2.50.0.18765): when a virtual service has pre-processing rules, showvs and
// listvs emit the field that follows the array inside it, without a comma:
//
//	"PreProcessRules": [
//	"rule_a",
//	"rule_b"
//	 "EspEnabled" : false,
//	]
//
// Group 1 is the array up to its last element, group 2 the misplaced field.
var misplacedPreProcessField = regexp.MustCompile(
	`("PreProcessRules"\s*:\s*\[(?:\s*"[^"]*"\s*,)*\s*"[^"]*")\s*("[^"]+"\s*:\s*[^,\[\]{}\r\n]+?)\s*,\s*\]`)

// repairJSON fixes known firmware JSON bugs. It is only applied to responses
// that are not valid JSON, so well-formed responses are never rewritten.
func repairJSON(raw []byte) []byte {
	// Close the array after its last element and move the field out after it.
	return misplacedPreProcessField.ReplaceAll(raw, []byte("$1 ], $2"))
}
