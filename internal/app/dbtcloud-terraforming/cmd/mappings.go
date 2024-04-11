package cmd

import "github.com/samber/lo"

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
