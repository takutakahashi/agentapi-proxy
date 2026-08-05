package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestSessionAffinityNormalizesYAMLMaps(t *testing.T) {
	value := map[string]interface{}{
		"nodeAffinity": map[interface{}]interface{}{
			"preferredDuringSchedulingIgnoredDuringExecution": []interface{}{
				map[interface{}]interface{}{
					"weight": 100,
					"preference": map[interface{}]interface{}{
						"matchExpressions": []interface{}{
							map[interface{}]interface{}{
								"key":      "kubernetes.io/hostname",
								"operator": "In",
								"values":   []interface{}{"ai-worker"},
							},
						},
					},
				},
			},
		},
	}

	affinity, err := sessionAffinity(value)
	require.NoError(t, err)
	require.NotNil(t, affinity)
	require.NotNil(t, affinity.NodeAffinity)
	terms := affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
	require.Len(t, terms, 1)
	assert.Equal(t, int32(100), terms[0].Weight)
	assert.Equal(t, []corev1.NodeSelectorRequirement{{
		Key:      "kubernetes.io/hostname",
		Operator: corev1.NodeSelectorOpIn,
		Values:   []string{"ai-worker"},
	}}, terms[0].Preference.MatchExpressions)
}

func TestSessionAffinityRejectsNonStringMapKey(t *testing.T) {
	_, err := sessionAffinity(map[string]interface{}{
		"nodeAffinity": map[interface{}]interface{}{1: "invalid"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "affinity map key must be a string")
}
