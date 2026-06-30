package exporter

import "bytes"

const (
	sipSchemeLen  = 4
	sipsSchemeLen = 5
)

// ParseURI extracts the user and host parts from a SIP URI found in From/To
// headers. Accepts forms: <sip:user@host:port;params>, "display"
// <sip:user@host>, "user"@host, user@host. Returns zero-copy subslices of the
// input. Parts that cannot be determined are returned as nil.
func ParseURI(value []byte) ([]byte, []byte) {
	uri := value

	if lt := bytes.IndexByte(uri, '<'); lt != -1 {
		if gt := bytes.IndexByte(uri, '>'); gt > lt {
			uri = uri[lt+1 : gt]
		}
	} else if semi := bytes.IndexByte(uri, ';'); semi != -1 {
		uri = uri[:semi]
	}

	uri = stripScheme(uri)

	at := bytes.IndexByte(uri, '@')
	if at == -1 {
		return nil, extractHost(uri)
	}

	return extractUser(uri[:at]), extractHost(uri[at+1:])
}

func stripScheme(uri []byte) []byte {
	if len(uri) >= sipsSchemeLen && bytes.EqualFold(uri[:sipsSchemeLen], []byte("sips:")) {
		return uri[sipsSchemeLen:]
	}
	if len(uri) >= sipSchemeLen && bytes.EqualFold(uri[:sipSchemeLen], []byte("sip:")) {
		return uri[sipSchemeLen:]
	}
	return uri
}

func stripParams(part []byte) []byte {
	if semi := bytes.IndexByte(part, ';'); semi != -1 {
		return part[:semi]
	}
	return part
}

func extractUser(part []byte) []byte {
	part = stripParams(part)
	if colon := bytes.IndexByte(part, ':'); colon != -1 {
		part = part[:colon]
	}
	part = bytes.TrimSpace(part)
	if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
		part = part[1 : len(part)-1]
	}
	return part
}

func extractHost(part []byte) []byte {
	part = stripParams(part)
	if len(part) > 0 && part[0] == '[' {
		if rb := bytes.IndexByte(part, ']'); rb != -1 {
			return part[1:rb]
		}
	}
	if colon := bytes.IndexByte(part, ':'); colon != -1 {
		part = part[:colon]
	}
	return part
}
