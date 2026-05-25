package normalize

import "strings"

func NigerianPhone(phone string) string {
	phone = strings.TrimSpace(phone)

	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")

	switch {

	case strings.HasPrefix(phone, "0"):
		return "+234" + phone[1:]

	case strings.HasPrefix(phone, "234"):
		return "+" + phone

	default:
		return phone
	}
}

