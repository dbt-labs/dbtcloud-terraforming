package cmd

import (
	"strings"

	"github.com/samber/lo"
)

func mapJobStatusCodeToText(status []any) []string {

	JobCompletionTriggerConditionsMappingCodeHuman := map[float64]string{
		10: "success",
		20: "error",
		30: "canceled",
	}
	return lo.Map(status, func(s any, _ int) string {
		return JobCompletionTriggerConditionsMappingCodeHuman[s.(float64)]
	})
}

// get the left part of a string until the last _
func getAdapterFromAdapterVersion(str string) string {

	adapter := str[:strings.LastIndex(str, "_")]
	if adapter == "trino" {
		return "starburst"
	}
	return adapter
}
