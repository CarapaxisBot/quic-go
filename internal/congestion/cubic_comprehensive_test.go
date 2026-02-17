package congestion

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/stretchr/testify/require"
)

// TestCubicSenderConfig tests the configurable congestion control
func TestCubicSenderConfig(t *testing.T) {
	t.Run("NewReno mode", func(t *testing.T) {
		var clock mockClock
		rttStats := utils.RTTStats{}
		rttStats.UpdateRTT(50*time.Millisecond, 0)

		sender := newCubicSender(
			&clock,
			&rttStats,
			&utils.ConnectionStats{},
			true, // reno = true (NewReno)
			protocol.InitialPacketSize,
			10*maxDatagramSize,
			100*maxDatagramSize,
			nil,
		)

		// Enter congestion avoidance
		for sender.CanSend(0) {
			sender.OnPacketSent(clock.Now(), 0, 1, maxDatagramSize, true)
		}
		sender.OnCongestionEvent(1, maxDatagramSize, 10*maxDatagramSize)

		initialWindow := sender.GetCongestionWindow()

		// Ack 3 RTTs worth
		for i := 0; i < 3; i++ {
			for j := 0; j < int(initialWindow/maxDatagramSize); j++ {
				sender.OnPacketAcked(protocol.PacketNumber(j+2), maxDatagramSize, 0, clock.Now())
			}
			clock.Advance(50 * time.Millisecond)
		}

		windowAfter3RTTs := sender.GetCongestionWindow()
		growth := windowAfter3RTTs - initialWindow

		// Reno: ~1 MSS per RTT = ~3 MSS for 3 RTTs
		require.InDelta(t, float64(3*maxDatagramSize), float64(growth), float64(2*maxDatagramSize),
			"Reno should grow ~linearly")
	})

	t.Run("CUBIC mode", func(t *testing.T) {
		var clock mockClock
		rttStats := utils.RTTStats{}
		rttStats.UpdateRTT(50*time.Millisecond, 0)

		sender := newCubicSender(
			&clock,
			&rttStats,
			&utils.ConnectionStats{},
			false, // reno = false (CUBIC)
			protocol.InitialPacketSize,
			10*maxDatagramSize,
			100*maxDatagramSize,
			nil,
		)

		// Enter congestion avoidance
		for sender.CanSend(0) {
			sender.OnPacketSent(clock.Now(), 0, 1, maxDatagramSize, true)
		}
		sender.OnCongestionEvent(1, maxDatagramSize, 10*maxDatagramSize)

		initialWindow := sender.GetCongestionWindow()

		// Ack 3 RTTs worth
		for i := 0; i < 3; i++ {
			for j := 0; j < int(initialWindow/maxDatagramSize); j++ {
				sender.OnPacketAcked(protocol.PacketNumber(j+2), maxDatagramSize, 0, clock.Now())
			}
			clock.Advance(50 * time.Millisecond)
		}

		windowAfter3RTTs := sender.GetCongestionWindow()
		growth := windowAfter3RTTs - initialWindow

		// CUBIC should grow faster than linear
		require.Greater(t, growth, protocol.ByteCount(3*maxDatagramSize),
			"CUBIC should grow faster than linear")
	})
}

// TestCubicSenderEdgeCases tests edge cases
func TestCubicSenderEdgeCases(t *testing.T) {
	t.Run("Minimum congestion window", func(t *testing.T) {
		sender := newTestCubicSender(true)
		sender.rttStats.UpdateRTT(50*time.Millisecond, 0)

		sender.SendAvailableSendWindow()
		sender.LoseNPackets(10)

		minWindow := sender.sender.GetCongestionWindow()
		require.GreaterOrEqual(t, minWindow, 2*maxDatagramSize,
			"Window should not go below 2 packets")
	})

	t.Run("Maximum congestion window", func(t *testing.T) {
		sender := newTestCubicSender(true)
		sender.rttStats.UpdateRTT(50*time.Millisecond, 0)

		for i := 0; i < 100; i++ {
			sender.SendAvailableSendWindow()
			sender.AckNPackets(10)
			sender.clock.Advance(50 * time.Millisecond)
			if sender.sender.GetCongestionWindow() >= 200*maxDatagramSize {
				break
			}
		}

		require.LessOrEqual(t, sender.sender.GetCongestionWindow(), 200*maxDatagramSize,
			"Window should be capped at maximum")
	})
}

// TestCubicSenderSlowStart tests slow start behavior
func TestCubicSenderSlowStart(t *testing.T) {
	t.Run("Exponential growth", func(t *testing.T) {
		sender := newTestCubicSender(true)
		sender.rttStats.UpdateRTT(50*time.Millisecond, 0)

		require.True(t, sender.sender.InSlowStart())

		initialWindow := sender.sender.GetCongestionWindow()

		for i := 0; i < 5; i++ {
			sender.SendAvailableSendWindow()
			sender.AckNPackets(int(initialWindow / maxDatagramSize))
			sender.clock.Advance(50 * time.Millisecond)
		}

		finalWindow := sender.sender.GetCongestionWindow()
		require.Greater(t, finalWindow, 2*initialWindow,
			"Slow start should grow exponentially")
	})

	t.Run("Exit on loss", func(t *testing.T) {
		sender := newTestCubicSender(true)
		sender.rttStats.UpdateRTT(50*time.Millisecond, 0)

		require.True(t, sender.sender.InSlowStart())
		sender.LosePacket(1)
		require.False(t, sender.sender.InSlowStart())
	})
}

// TestCubicSenderLossResponse tests response to packet loss
func TestCubicSenderLossResponse(t *testing.T) {
	t.Run("Multiple losses", func(t *testing.T) {
		sender := newTestCubicSender(true)
		sender.rttStats.UpdateRTT(50*time.Millisecond, 0)

		sender.SendAvailableSendWindow()
		sender.LoseNPackets(3)

		windowAfterLoss := sender.sender.GetCongestionWindow()
		require.Less(t, windowAfterLoss, 10*maxDatagramSize)
	})

	t.Run("Window reduction", func(t *testing.T) {
		sender := newTestCubicSender(true)
		sender.rttStats.UpdateRTT(50*time.Millisecond, 0)

		for i := 0; i < 10; i++ {
			sender.SendAvailableSendWindow()
			sender.AckNPackets(10)
			sender.clock.Advance(50 * time.Millisecond)
		}

		windowBefore := sender.sender.GetCongestionWindow()
		sender.LosePacket(1)
		windowAfter := sender.sender.GetCongestionWindow()

		require.Less(t, windowAfter, windowBefore)
	})
}
