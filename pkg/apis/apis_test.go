package apis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToScheme(t *testing.T) {
	scheme := runtime.NewScheme()
	err := AddToScheme(scheme)
	assert.Nil(t, err)
}
