package web

import "fmt"

func hugepageArg(value string) (string, error) {
	switch value {
	case "", "0":
		return "", nil
	case "2", "1024":
		return "hugepages,page.size=" + value, nil
	default:
		return "", fmt.Errorf("大页规格只支持 2 MiB 或 1 GiB")
	}
}
