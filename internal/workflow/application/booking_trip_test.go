package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfirmBookingAndCreateTrip_ValidatesWorkflowCommand(t *testing.T) {
	workflow := NewConfirmBookingAndCreateTrip(nil, nil, nil, nil, nil)
	_, err := workflow.Execute(context.Background(), ConfirmBookingAndCreateTripCommand{})
	require.Error(t, err)
}
