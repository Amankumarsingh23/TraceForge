package telemetry

import (
	"fmt"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"strconv"
	"strings"
)

// SamplingProfile is deliberately opt-in. The default preserves protected demos.
func SamplingProfile(profile, argument string) (sdktrace.Sampler, error) {
	switch profile {
	case "", "always_on":
		if argument != "" {
			return nil, fmt.Errorf("sampler argument requires parentbased_traceidratio")
		}
		return sdktrace.AlwaysSample(), nil
	case "parentbased_traceidratio":
		if argument == "" || strings.TrimSpace(argument) != argument {
			return nil, fmt.Errorf("sampling ratio required")
		}
		ratio, err := strconv.ParseFloat(argument, 64)
		if err != nil || !(ratio >= 0 && ratio <= 1) {
			return nil, fmt.Errorf("sampling ratio must be0..1")
		}
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), nil
	default:
		return nil, fmt.Errorf("unsupported local trace sampler")
	}
}
