package node

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// BodyEncodingForm sends a map body as application/x-www-form-urlencoded,
// encoded after templates resolve so that a resolved value such as
// `garbage+tag@nanostack.dev` keeps its `+`.
const BodyEncodingForm = "form"

const formContentType = "application/x-www-form-urlencoded"

// encodeFormBody nests maps and lists with brackets, as Stripe and Rails read
// them: {"metadata":{"plan":"pro"},"items":[{"price":"p_1"}]} becomes
// metadata[plan]=pro&items[0][price]=p_1. A string body is already encoded.
func encodeFormBody(body any) ([]byte, error) {
	if encoded, ok := body.(string); ok {
		return []byte(encoded), nil
	}
	fields, ok := body.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("a form body must be an object or a string, got %T", body)
	}
	values := url.Values{}
	for key, value := range fields {
		if err := addFormValue(values, key, value); err != nil {
			return nil, err
		}
	}
	return []byte(values.Encode()), nil
}

func addFormValue(values url.Values, key string, value any) error {
	switch typed := value.(type) {
	case nil:
		values.Add(key, "")
	case map[string]any:
		for child, childValue := range typed {
			if err := addFormValue(values, key+"["+child+"]", childValue); err != nil {
				return err
			}
		}
	case []any:
		for index, item := range typed {
			if err := addFormValue(values, key+"["+strconv.Itoa(index)+"]", item); err != nil {
				return err
			}
		}
	case string:
		values.Add(key, typed)
	case bool:
		values.Add(key, strconv.FormatBool(typed))
	case float64:
		values.Add(key, strconv.FormatFloat(typed, 'f', -1, 64))
	case int:
		values.Add(key, strconv.Itoa(typed))
	case int64:
		values.Add(key, strconv.FormatInt(typed, 10))
	case json.Number:
		values.Add(key, typed.String())
	default:
		return fmt.Errorf("form field %s: cannot encode %T", key, value)
	}
	return nil
}
