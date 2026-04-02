package paymentsgate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type orderedKind int

const (
	kindObject orderedKind = iota
	kindArray
	kindString
	kindNumber
	kindBool
	kindNull
)

type orderedField struct {
	key   string
	value orderedValue
}

type orderedValue struct {
	kind   orderedKind
	object []orderedField
	array  []orderedValue
	scalar string
}

type flattenEntry struct {
	Key   string
	Value string
}

func FlattenForSignature(body []byte) (string, error) {
	root, err := parseOrderedJSON(body)
	if err != nil {
		return "", err
	}

	entries := make([]flattenEntry, 0, 16)
	index := 0
	unwindOrderedValue(root, "", &index, &entries)

	sort.Slice(entries, func(i, j int) bool {
		return naturalLess(entries[i].Key, entries[j].Key)
	})

	var builder strings.Builder
	for _, entry := range entries {
		builder.WriteString(entry.Value)
	}
	return builder.String(), nil
}

func ChecksumSHA256(body []byte) (string, error) {
	flat, err := FlattenForSignature(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(flat))
	return hex.EncodeToString(sum[:]), nil
}

func parseOrderedJSON(body []byte) (orderedValue, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return orderedValue{}, ErrEmptyBody
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()

	value, err := parseOrderedValue(decoder)
	if err != nil {
		return orderedValue{}, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return value, nil
}

func parseOrderedValue(decoder *json.Decoder) (orderedValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return orderedValue{}, err
	}

	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			value := orderedValue{kind: kindObject}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return orderedValue{}, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return orderedValue{}, fmt.Errorf("unexpected object key token %T", keyToken)
				}
				child, err := parseOrderedValue(decoder)
				if err != nil {
					return orderedValue{}, err
				}
				value.object = append(value.object, orderedField{key: key, value: child})
			}
			if _, err := decoder.Token(); err != nil {
				return orderedValue{}, err
			}
			return value, nil
		case '[':
			value := orderedValue{kind: kindArray}
			for decoder.More() {
				child, err := parseOrderedValue(decoder)
				if err != nil {
					return orderedValue{}, err
				}
				value.array = append(value.array, child)
			}
			if _, err := decoder.Token(); err != nil {
				return orderedValue{}, err
			}
			return value, nil
		default:
			return orderedValue{}, fmt.Errorf("unexpected json delimiter %q", string(typed))
		}
	case string:
		return orderedValue{kind: kindString, scalar: typed}, nil
	case json.Number:
		return orderedValue{kind: kindNumber, scalar: typed.String()}, nil
	case bool:
		if typed {
			return orderedValue{kind: kindBool, scalar: "true"}, nil
		}
		return orderedValue{kind: kindBool, scalar: "false"}, nil
	case nil:
		return orderedValue{kind: kindNull, scalar: ""}, nil
	default:
		return orderedValue{}, fmt.Errorf("unsupported json token %T", token)
	}
}

func unwindOrderedValue(value orderedValue, key string, index *int, out *[]flattenEntry) {
	switch value.kind {
	case kindObject:
		for _, field := range value.object {
			unwindOrderedValue(field.value, field.key, index, out)
		}
	case kindArray:
		for i, child := range value.array {
			unwindOrderedValue(child, strconv.Itoa(i), index, out)
		}
	default:
		*index = *index + 1
		entryKey := strings.ToLower(key + "_" + strconv.Itoa(*index))
		*out = append(*out, flattenEntry{Key: entryKey, Value: value.scalar})
	}
}

func naturalLess(left, right string) bool {
	if left == right {
		return false
	}

	leftRunes := []rune(left)
	rightRunes := []rune(right)
	i, j := 0, 0
	for i < len(leftRunes) && j < len(rightRunes) {
		lr := leftRunes[i]
		rr := rightRunes[j]

		if unicode.IsDigit(lr) && unicode.IsDigit(rr) {
			li := i
			for i < len(leftRunes) && unicode.IsDigit(leftRunes[i]) {
				i++
			}
			rj := j
			for j < len(rightRunes) && unicode.IsDigit(rightRunes[j]) {
				j++
			}

			ln := strings.TrimLeft(string(leftRunes[li:i]), "0")
			rn := strings.TrimLeft(string(rightRunes[rj:j]), "0")
			if ln == "" {
				ln = "0"
			}
			if rn == "" {
				rn = "0"
			}
			if len(ln) != len(rn) {
				return len(ln) < len(rn)
			}
			if ln != rn {
				return ln < rn
			}
			continue
		}

		ll := unicode.ToLower(lr)
		rl := unicode.ToLower(rr)
		if ll != rl {
			return ll < rl
		}
		i++
		j++
	}
	return len(leftRunes) < len(rightRunes)
}
