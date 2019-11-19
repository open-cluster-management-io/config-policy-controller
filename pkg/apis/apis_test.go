package apis

import (
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

func TestAddToScheme(t *testing.T)  {
	scheme := runtime.NewScheme()
	err := AddToScheme(scheme)
	assert.Nil(t, err)
}

