package geoip

//go:generate go run gen_e164.go -xml data/PhoneNumberMetadata.xml -out e164_generated.go

const unknownDestination = "unknown"

// LookupDestination resolves a phone number to an ISO 3166-1 alpha-2 country
// code using E.164 prefix matching. Numbers starting with "+" or "00" are
// treated as international; others fall back to localCountry (if non-empty) or
// "unknown".
func LookupDestination(phoneNumber string, localCountry string) string {
	if phoneNumber == "" {
		return unknownDestination
	}

	digits := extractDigits(phoneNumber)

	intlDigits, ok := stripInternationalPrefix(digits)
	if ok {
		return matchPrefix(intlDigits)
	}

	if localCountry != "" {
		return localCountry
	}

	return unknownDestination
}

// stripInternationalPrefix returns the digit string after "+" or "00" prefix.
// If neither prefix is present, ok is false.
func stripInternationalPrefix(digits string) (string, bool) {
	if len(digits) == 0 {
		return "", false
	}
	if digits[0] == '+' {
		return digits[1:], true
	}
	if len(digits) >= 2 && digits[0] == '0' && digits[1] == '0' {
		return digits[2:], true
	}
	return "", false
}

// matchPrefix performs longest-prefix lookup against the E.164 prefix table.
func matchPrefix(digits string) string {
	for i := len(digits); i >= 1; i-- {
		if iso, ok := e164PrefixTable[digits[:i]]; ok {
			return iso
		}
	}
	return unknownDestination
}

// extractDigits removes non-digit characters, keeping '+' for prefix detection.
func extractDigits(s string) string {
	var b []byte
	for i := range len(s) {
		c := s[i]
		if c >= '0' && c <= '9' || c == '+' {
			b = append(b, c)
		}
	}
	return string(b)
}
