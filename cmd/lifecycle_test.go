// Copyright 2026 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"strings"
	"testing"
	"time"
)

// ── statusResult.String() BlockRef rendering tests ──────────────────────

// TestStatusResultString_NoTipFields checks that when no tip fields are set,
// the String output does not include tip information.
func TestStatusResultString_NoTipFields(t *testing.T) {
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
	}
	out := r.String()
	if strings.Contains(out, "tip") {
		t.Errorf("expected no tip info when all fields are nil, got: %s", out)
	}
	if !strings.Contains(out, "synced") {
		t.Errorf("expected sync status, got: %s", out)
	}
}

// TestStatusResultString_SlotOnly checks that a tip with only slot set renders
// correctly, not dropping the tip data.
func TestStatusResultString_SlotOnly(t *testing.T) {
	slot := uint64(12345)
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipSlot: &slot,
	}
	out := r.String()
	if !strings.Contains(out, "tip") {
		t.Errorf("expected tip info, got: %s", out)
	}
	if !strings.Contains(out, "slot: 12345") {
		t.Errorf("expected slot in output, got: %s", out)
	}
	if strings.Contains(out, "block:") || strings.Contains(out, "hash:") {
		t.Errorf("expected only slot in tip, got: %s", out)
	}
}

// TestStatusResultString_HashOnly checks that a tip with only hash set renders
// correctly. Bark's BlockRef contract permits hash-only tips.
func TestStatusResultString_HashOnly(t *testing.T) {
	hash := "deadbeef0123456789abcdef"
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipHash: &hash,
	}
	out := r.String()
	if !strings.Contains(out, "tip") {
		t.Errorf("expected tip info for hash-only tip, got: %s", out)
	}
	if !strings.Contains(out, "hash: deadbeef") {
		t.Errorf("expected hash in output, got: %s", out)
	}
	if strings.Contains(out, "slot:") || strings.Contains(out, "block:") {
		t.Errorf("expected only hash in tip, got: %s", out)
	}
}

// TestStatusResultString_BlockNumberOnly checks that a tip with only block_number
// set renders correctly. This prevents dropping valid tip data when only block
// number is available.
func TestStatusResultString_BlockNumberOnly(t *testing.T) {
	blockNum := uint64(9876)
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipBlockNumber: &blockNum,
	}
	out := r.String()
	if !strings.Contains(out, "tip") {
		t.Errorf("expected tip info for block-number-only tip, got: %s", out)
	}
	if !strings.Contains(out, "block: 9876") {
		t.Errorf("expected block number in output, got: %s", out)
	}
	if strings.Contains(out, "slot:") || strings.Contains(out, "hash:") {
		t.Errorf("expected only block number in tip, got: %s", out)
	}
}

// TestStatusResultString_SlotAndHash checks that a tip with slot and hash renders
// both fields correctly with proper separators.
func TestStatusResultString_SlotAndHash(t *testing.T) {
	slot := uint64(12345)
	hash := "abcdef1234567890"
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipSlot: &slot,
		TipHash: &hash,
	}
	out := r.String()
	if !strings.Contains(out, "slot: 12345") {
		t.Errorf("expected slot in output, got: %s", out)
	}
	if !strings.Contains(out, "hash: abcdef") {
		t.Errorf("expected hash in output, got: %s", out)
	}
	// Check that fields are separated by comma
	if !strings.Contains(out, ", ") {
		t.Errorf("expected comma separator between fields, got: %s", out)
	}
}

// TestStatusResultString_AllTipFields checks that when all three tip fields are
// present, they all render correctly with proper separators.
func TestStatusResultString_AllTipFields(t *testing.T) {
	slot := uint64(12345)
	blockNum := uint64(9876)
	hash := "fedcba9876543210"
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipSlot:        &slot,
		TipBlockNumber: &blockNum,
		TipHash:        &hash,
	}
	out := r.String()
	if !strings.Contains(out, "slot: 12345") {
		t.Errorf("expected slot in output, got: %s", out)
	}
	if !strings.Contains(out, "block: 9876") {
		t.Errorf("expected block number in output, got: %s", out)
	}
	if !strings.Contains(out, "hash: fedcba") {
		t.Errorf("expected hash in output, got: %s", out)
	}
}

// ── statusResult.TableRows() BlockRef rendering tests ───────────────────

// TestStatusResultTableRows_NoTipFields checks that when no tip fields are set,
// the table shows "N/A" for tip info.
func TestStatusResultTableRows_NoTipFields(t *testing.T) {
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// Tip info is at index 5
	tipInfo := rows[0][5]
	if tipInfo != "N/A" {
		t.Errorf("expected N/A for tip when no fields set, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_SlotOnly checks that a slot-only tip is rendered
// in the table, not displayed as N/A.
func TestStatusResultTableRows_SlotOnly(t *testing.T) {
	slot := uint64(12345)
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipSlot: &slot,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "slot:12345") {
		t.Errorf("expected slot in tip info, got: %s", tipInfo)
	}
	if strings.Contains(tipInfo, "N/A") {
		t.Errorf("expected slot-only tip to not be N/A, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_HashOnly checks that a hash-only tip is rendered
// in the table. This validates the fix for dropping valid tip data.
func TestStatusResultTableRows_HashOnly(t *testing.T) {
	hash := "deadbeef0123456789"
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipHash: &hash,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "hash:deadbeef") {
		t.Errorf("expected hash in tip info, got: %s", tipInfo)
	}
	if tipInfo == "N/A" {
		t.Errorf("hash-only tip should not be N/A, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_BlockNumberOnly checks that a block-number-only tip
// is rendered in the table. Bark's BlockRef permits this combination.
func TestStatusResultTableRows_BlockNumberOnly(t *testing.T) {
	blockNum := uint64(9876)
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipBlockNumber: &blockNum,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "block:9876") {
		t.Errorf("expected block number in tip info, got: %s", tipInfo)
	}
	if tipInfo == "N/A" {
		t.Errorf("block-number-only tip should not be N/A, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_AllTipFields checks that when all three tip fields
// are present, they all appear in the table output.
func TestStatusResultTableRows_AllTipFields(t *testing.T) {
	slot := uint64(12345)
	blockNum := uint64(9876)
	hash := "fedcba9876543210"
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipSlot:        &slot,
		TipBlockNumber: &blockNum,
		TipHash:        &hash,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "slot:12345") {
		t.Errorf("expected slot in tip info, got: %s", tipInfo)
	}
	if !strings.Contains(tipInfo, "block:9876") {
		t.Errorf("expected block number in tip info, got: %s", tipInfo)
	}
	if !strings.Contains(tipInfo, "hash:fedcba") {
		t.Errorf("expected hash in tip info, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_HashAndBlockNumber checks a combination without slot.
func TestStatusResultTableRows_HashAndBlockNumber(t *testing.T) {
	blockNum := uint64(5555)
	hash := "cafebabe"
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipBlockNumber: &blockNum,
		TipHash:        &hash,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "block:5555") {
		t.Errorf("expected block number in tip info, got: %s", tipInfo)
	}
	if !strings.Contains(tipInfo, "hash:cafebabe") {
		t.Errorf("expected hash in tip info, got: %s", tipInfo)
	}
	if strings.Contains(tipInfo, "slot:") {
		t.Errorf("should not contain slot, got: %s", tipInfo)
	}
}
