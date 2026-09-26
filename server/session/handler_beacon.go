package session

import (
	"fmt"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/entity/effect"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// beaconInputSlot is the slot index of the input item in the beacon.
const beaconInputSlot = 0x1b

// handleBeaconPayment handles the selection of effects in a beacon and the removal of the item used to pay
// for those effects.
func (h *ItemStackRequestHandler) handleBeaconPayment(a *protocol.BeaconPaymentStackRequestAction, s *Session, tx *world.Tx) error {
	slot := protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerBeaconPayment},
		Slot:      beaconInputSlot,
	}
	// First check if there actually is a beacon opened.
	if !s.containerOpened.Load() {
		return fmt.Errorf("no beacon container opened")
	}
	pos := *s.openedPos.Load()
	beacon, ok := tx.Block(pos).(block.Beacon)
	if !ok {
		return fmt.Errorf("no beacon container opened")
	}

	// Check if the item present in the beacon slot is valid.
	payment, _ := h.itemInSlot(slot, s, tx)
	if payable, ok := payment.Item().(item.BeaconPayment); !ok || !payable.PayableForBeacon() {
		return fmt.Errorf("item %#v in beacon slot cannot be used as payment", payment)
	}

	// beacon is a copy that nothing below writes to, so every read of its level
	// gave this number. Reading it once also stops the whole beacon being
	// copied into validBeaconEffect on each call.
	level := beacon.Level()

	// Check if the effects are valid and allowed for the beacon's level.
	if !h.validBeaconEffect(a.PrimaryEffect, level) {
		return fmt.Errorf("primary effect selected is not allowed: %v for level %v", a.PrimaryEffect, level)
	} else if !h.validBeaconEffect(a.SecondaryEffect, level) || (level < 4 && a.SecondaryEffect != 0) {
		return fmt.Errorf("secondary effect selected is not allowed: %v for level %v", a.SecondaryEffect, level)
	}

	primary, pOk := effect.ByID(int(a.PrimaryEffect))
	secondary, sOk := effect.ByID(int(a.SecondaryEffect))
	if pOk {
		beacon.Primary = primary.(effect.LastingType)
	}
	if sOk {
		beacon.Secondary = secondary.(effect.LastingType)
	}
	tx.SetBlock(pos, beacon, nil)

	// The client will send a Destroy action after this action, but we can't rely on that because the client
	// could just not send it.
	// We just ignore the next Destroy action and set the item to air here.
	// Which input was the problem: a payment stack of N was cleared
	// wholesale, while vanilla consumes exactly 1 item per effect selection.
	// Growing by -1 keeps the item type and leaves N-1 behind, so a 1x
	// payment still clears to air exactly as before.
	h.setItemInSlot(slot, payment.Grow(-1), s, tx)
	h.ignoreDestroy = true
	return nil
}

// validBeaconEffect checks if the ID passed is a valid beacon effect.
func (h *ItemStackRequestHandler) validBeaconEffect(id int32, level int) bool {
	switch id {
	case 1, 3:
		return level >= 1
	case 8, 11:
		return level >= 2
	case 5:
		return level >= 3
	case 10:
		return level >= 4
	case 0:
		return true
	}
	return false
}
