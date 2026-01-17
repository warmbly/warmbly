package utils

func GetSignatureHTML(name string) string {
	return "<p><b>Best Regards,</b></p><p>" + name + "</p>"
}

func GetSignaturePlain(name string) string {
	return "Best Regards,\n" + name
}
