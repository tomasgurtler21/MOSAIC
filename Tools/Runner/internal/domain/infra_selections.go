package domain

import (
	"fmt"
	"sort"
)

// ValidateInfraSelections checks an infrastructure_selections map against the
// declared agents. Every gated class with more than one declaration must have
// an entry; every key must be a gated class; every value must name a declared
// agent; and the named agent's class must equal the key. An entry for a class
// with a single declaration is accepted when valid. The selection is never
// inferred from declaration order. Failures are *RefusalError.
func ValidateInfraSelections(declared []DeclaredInfraAgent, selections map[string]string) error {
	refuse := func(reason string) error {
		return &RefusalError{Component: "domain", Resource: "infrastructure_selections", Reason: reason}
	}

	classCount := make(map[string]int)
	classByName := make(map[string]string, len(declared))
	for _, a := range declared {
		classByName[a.Name] = a.Class
		if IsGatedInfraClass(a.Class) {
			classCount[a.Class]++
		}
	}

	classes := make([]string, 0, len(classCount))
	for class := range classCount {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	for _, class := range classes {
		if classCount[class] > 1 {
			if _, ok := selections[class]; !ok {
				return refuse(fmt.Sprintf(
					"missing selection for class %s, which declares %d agents; select one "+
						"(--infra-class %s=<agent>); the selection cannot be inferred from declaration order",
					class, classCount[class], class))
			}
		}
	}

	keys := make([]string, 0, len(selections))
	for class := range selections {
		keys = append(keys, class)
	}
	sort.Strings(keys)
	for _, class := range keys {
		name := selections[class]
		if !IsGatedInfraClass(class) {
			return refuse(fmt.Sprintf("unknown class %s", class))
		}
		declaredClass, known := classByName[name]
		if !known {
			return refuse(fmt.Sprintf("unknown agent %s for class %s: no such agent is declared", name, class))
		}
		if declaredClass != class {
			return refuse(fmt.Sprintf("agent %s is class %s, not %s", name, declaredClass, class))
		}
	}
	return nil
}
