package miner

import (
	"time"
)

// threadedMine starts a gothread that does CPU mining. threadedMine is the
// only function that should be setting the mining flag to true.
func (m *Miner) threadedMine() {
	// There should not be another thread mining, and mining should be enabled.
	m.mu.Lock()
	if m.mining || !m.miningOn {
		m.mu.Unlock()
		return
	}
	m.mining = true
	m.mu.Unlock()

	// Solve blocks repeatedly, keeping track of how fast hashing is occuring.
	cycleStart := time.Now()
	for {
		// Kill the thread if mining has been turned off.
		m.mu.Lock()
		if !m.miningOn {
			m.mining = false
			m.mu.Unlock()
			return
		}
		bfw := m.blockForWork()
		target := m.persist.Target
		m.mu.Unlock()

		// Grab a block and try to solve it.
		b, solved := m.SolveBlock(bfw, target)
		if solved {
			err := m.managedSubmitBlock(b)
			if err != nil {
				m.log.Println("ERROR: An error occurred while cpu mining:", err)
			}
		}

		// Update the hashrate. If the block was solved, the full set of
		// iterations was not completed, so the hashrate should not be updated.
		m.mu.Lock()
		if !solved {
			nanosecondsElapsed := 1 + time.Since(cycleStart).Nanoseconds() // Add 1 to prevent divide by zero errors.
			cycleStart = time.Now()                                        // Reset the cycle counter as soon as the previous value is measured.
			m.hashRate = 1e9 * solveAttempts / nanosecondsElapsed
		}
		m.mu.Unlock()
	}
}

// CPUHashrate returns an estimated cpu hashrate.
func (m *Miner) CPUHashrate() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int(m.hashRate)
}

// CPUMining indicates whether the cpu miner is running.
func (m *Miner) CPUMining() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.miningOn
}

// StartCPUMining will start a single threaded cpu miner. If the miner is
// already running, nothing will happen.
func (m *Miner) StartCPUMining() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.miningOn = true
	go m.threadedMine()
}

// StopCPUMining will stop the cpu miner. If the cpu miner is already stopped,
// nothing will happen.
func (m *Miner) StopCPUMining() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hashRate = 0
	m.miningOn = false
}
