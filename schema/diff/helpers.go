package diff



func IndexEqual(a []string, b []string) bool {

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func ifUnique(u bool) string {
	if u {
		return "UNIQUE"
	}
	return ""
}