package session

import (
	"fmt"
	"unicode/utf8"

	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/entity"
	"github.com/jv2w/Impulse/server/event"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"math"
	"slices"
	"time"
)

// maxStackRequests bounds the Requests a single ItemStackRequest packet may
// carry. A legitimate interaction batches one or two requests: 16 is several
// times that while keeping one packet from fanning out to hundreds of
// inventory mutations on the tick path.
const maxStackRequests = 16

// maxStackActions bounds the Actions a single stack request may carry. A
// legitimate request carries a handful of Take/Place/Craft steps: 32 is
// several times that.
const maxStackActions = 32

// maxFilterStrings bounds the FilterStrings a single stack request may carry,
// and maxFilterStringLen bounds each string. The anvil rename path persists
// one string to item NBT, mirroring the 256-byte book signing cap.
const (
	maxFilterStrings   = 8
	maxFilterStringLen = 256
)

// ItemStackRequestHandler handles the ItemStackRequest packet. It handles the actions done within the
// inventory.
type ItemStackRequestHandler struct {
	currentRequest int32

	changes         map[byte]map[byte]changeInfo
	responseChanges map[int32]map[*inventory.Inventory]map[byte]responseChange

	pendingResults []item.Stack

	current       time.Time
	ignoreDestroy bool

	// originals holds the contents each slot had before the first write to it
	// since the start of the request or its last checkpoint. It is keyed by the
	// inventory written, not the container ID the client named, because several
	// IDs name the same inventory and the offhand maps every slot to 0.
	originals []slotOriginal
	// committed holds the inventories whose changes were made final by a
	// checkpoint in the current request. The client reverts them on a reject
	// all the same, so they are sent to it again.
	committed []*inventory.Inventory
	// effects holds the effects outside the inventories that the action being
	// handled asked for. They run only once the action has succeeded.
	effects []func()
	// checkpointed is set by an action whose outcome cannot be undone, such as
	// an item spawned by a drop, once that outcome has happened.
	checkpointed bool
	// writeErr holds the first write in the current action that the inventory
	// refused, for example an item its slot validator does not accept.
	writeErr error
}

// slotOriginal is the contents of slot in inv before the first write to it.
type slotOriginal struct {
	inv    *inventory.Inventory
	slot   int
	before item.Stack
}

// responseChange represents a change in a specific item stack response. It holds the timestamp of the
// response which is used to get rid of changes that the client will have received.
type responseChange struct {
	id        int32
	timestamp time.Time
}

// changeInfo holds information on a slot change initiated by an item stack request. It holds both the new and the old
// item information and is used for reverting and verifying.
type changeInfo struct {
	after  protocol.StackResponseSlotInfo
	before item.Stack
}

// Handle ...
func (h *ItemStackRequestHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.ItemStackRequest)
	h.current = time.Now()

	// Which input was the problem: Requests, Actions and FilterStrings were
	// ranged with no length check, so one packet fanned out to hundreds of
	// inventory mutations plus unbounded rename strings persisted to NBT.
	if len(pk.Requests) > maxStackRequests {
		return fmt.Errorf("too many stack requests: %d", len(pk.Requests))
	}
	for _, req := range pk.Requests {
		if err := validateStackRequest(req); err != nil {
			return err
		}
	}

	s.inTransaction.Store(true)
	defer s.inTransaction.Store(false)

	for _, req := range pk.Requests {
		if err := h.handleRequest(req, s, tx, c); err != nil {
			// Item stacks being out of sync isn't uncommon, so don't error. Just debug the error and let the
			// revert do its work.
			s.conf.Log.Debug("process packet: ItemStackRequest: resolve item stack request: " + err.Error())
		}
	}
	return nil
}

// validateStackRequest bounds one ItemStackRequest's Actions and FilterStrings
// before any inventory mutation runs. It is shared by the packet handler and
// the PlayerAuthInput embedded-request path, so both reject the same inputs.
func validateStackRequest(req protocol.ItemStackRequest) error {
	if len(req.Actions) > maxStackActions {
		return fmt.Errorf("too many stack actions: %d", len(req.Actions))
	}
	if len(req.FilterStrings) > maxFilterStrings {
		return fmt.Errorf("too many filter strings: %d", len(req.FilterStrings))
	}
	for _, fs := range req.FilterStrings {
		if len(fs) > maxFilterStringLen {
			return fmt.Errorf("filter string can not be longer than 256 bytes")
		}
		if !utf8.ValidString(fs) {
			return fmt.Errorf("filter string must be valid utf8")
		}
	}
	return nil
}

// handleRequest resolves a single item stack request from the client.
func (h *ItemStackRequestHandler) handleRequest(req protocol.ItemStackRequest, s *Session, tx *world.Tx, c Controllable) (err error) {
	h.currentRequest = req.RequestID
	// The PlayerAuthInput path reaches here without Handle, so the time the
	// response changes below are stamped with is taken per request.
	h.current = time.Now()
	defer func() {
		if err != nil {
			h.reject(req.RequestID, s, tx)
			return
		}
		h.resolve(req.RequestID, s)
		h.ignoreDestroy = false
	}()

	// Which input was the problem: requests made only of Craft actions, which
	// never verify a slot. The cap and the expiry of response changes lived in
	// verifySlot alone, so those requests grew responseChanges without bound.
	h.expireResponseChanges()
	if len(h.responseChanges) > 256 {
		return fmt.Errorf("too many unacknowledged request slot changes")
	}

	for _, action := range req.Actions {
		switch a := action.(type) {
		case *protocol.TakeStackRequestAction:
			err = h.handleTake(a, s, tx, c)
		case *protocol.PlaceStackRequestAction:
			err = h.handlePlace(a, s, tx, c)
		case *protocol.SwapStackRequestAction:
			err = h.handleSwap(a, s, tx, c)
		case *protocol.DestroyStackRequestAction:
			err = h.handleDestroy(a, s, tx, c)
		case *protocol.DropStackRequestAction:
			err = h.handleDrop(a, s, tx, c)
		case *protocol.BeaconPaymentStackRequestAction:
			err = h.handleBeaconPayment(a, s, tx)
		case *protocol.CraftRecipeStackRequestAction:
			if s.containerOpened.Load() {
				var special bool
				switch tx.Block(*s.openedPos.Load()).(type) {
				case block.SmithingTable:
					err, special = h.handleSmithing(a, s, tx), true
				case block.Stonecutter:
					err, special = h.handleStonecutting(a, s, tx), true
				case block.EnchantingTable:
					err, special = h.handleEnchant(a, s, tx, c), true
				}
				if special {
					// This was a "special action" and was handled, so we can move onto the next action.
					break
				}
			}
			err = h.handleCraft(a, s, tx)
		case *protocol.AutoCraftRecipeStackRequestAction:
			err = h.handleAutoCraft(a, s, tx)
		case *protocol.CraftRecipeOptionalStackRequestAction:
			err = h.handleCraftRecipeOptional(a, s, req.FilterStrings, c, tx)
		case *protocol.CraftLoomRecipeStackRequestAction:
			err = h.handleLoomCraft(a, s, tx)
		case *protocol.CraftGrindstoneRecipeStackRequestAction:
			err = h.handleGrindstoneCraft(s, tx, c)
		case *protocol.CraftCreativeStackRequestAction:
			err = h.handleCreativeCraft(a, s, tx, c)
		case *protocol.MineBlockStackRequestAction:
			err = h.handleMineBlock(a, s, tx)
		case *protocol.CreateStackRequestAction:
			err = h.handleCreate(a, s, tx)
		case *protocol.ConsumeStackRequestAction, *protocol.CraftResultsDeprecatedStackRequestAction:
			// Don't do anything with this.
		default:
			return fmt.Errorf("unhandled stack request action %#v", action)
		}
		if err == nil {
			err = h.writeErr
		}
		if err != nil {
			clear(h.effects)
			h.effects = h.effects[:0]
			err = fmt.Errorf("%T: %w", action, err)
			return
		}
		h.commitAction()
	}
	return
}

// commitAction runs the effects the action just handled asked for and, if it
// had any or could not be undone, makes the changes so far final.
//
// Which input was the problem: a request whose later action fails. Its earlier
// actions had already spawned items, experience orbs and blocks, and the
// revert put their inputs back, so a Drop followed by a bad action kept both
// the dropped items and the stack. Effects now happen only for an action that
// succeeded, and once they have happened the revert stops at that point.
func (h *ItemStackRequestHandler) commitAction() {
	effects := len(h.effects) > 0
	for i, f := range h.effects {
		f()
		h.effects[i] = nil
	}
	h.effects = h.effects[:0]
	if effects || h.checkpointed {
		h.checkpoint()
	}
}

// checkpoint makes every change of the current request so far final: a later
// reject leaves them in place and sends the inventories they touched again.
func (h *ItemStackRequestHandler) checkpoint() {
	for _, o := range h.originals {
		if !slices.Contains(h.committed, o.inv) {
			h.committed = append(h.committed, o.inv)
		}
	}
	clear(h.originals)
	h.originals = h.originals[:0]
	h.checkpointed = false
}

// effect queues f to run once the action being handled has succeeded.
func (h *ItemStackRequestHandler) effect(f func()) {
	h.effects = append(h.effects, f)
}

// handleTake handles a Take stack request action.
func (h *ItemStackRequestHandler) handleTake(a *protocol.TakeStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	return h.handleTransfer(a.Source, a.Destination, a.Count, s, tx, c)
}

// handlePlace handles a Place stack request action.
func (h *ItemStackRequestHandler) handlePlace(a *protocol.PlaceStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	return h.handleTransfer(a.Source, a.Destination, a.Count, s, tx, c)
}

// handleTransfer handles the transferring of x count from a source slot to a destination slot.
func (h *ItemStackRequestHandler) handleTransfer(from, to protocol.StackRequestSlotInfo, count byte, s *Session, tx *world.Tx, c Controllable) error {
	if err := h.verifySlots(s, tx, from, to); err != nil {
		return fmt.Errorf("source slot out of sync: %w", err)
	}
	i, _ := h.itemInSlot(from, s, tx)
	dest, _ := h.itemInSlot(to, s, tx)
	if !i.Comparable(dest) {
		return fmt.Errorf("client tried transferring %v to %v, but the stacks are incomparable", i, dest)
	}
	if i.Count() < int(count) {
		return fmt.Errorf("client tried subtracting %v from item count, but there are only %v", count, i.Count())
	}
	// dest is not written to until after both checks, so the reads below all
	// gave these values. MaxCount type asserts the item and Empty walks into
	// its EncodeItem, so each is asked once instead of up to twice.
	destMax, destEmpty := dest.MaxCount(), dest.Empty()
	if (dest.Count()+int(count) > destMax) && !destEmpty {
		return fmt.Errorf("client tried adding %v to item count %v, but max is %v", count, dest.Count(), destMax)
	}
	if destEmpty {
		dest = i.Grow(-math.MaxInt32)
	}

	invA, _ := s.invByID(int32(from.Container.ContainerID), tx)
	invB, _ := s.invByID(int32(to.Container.ContainerID), tx)

	ctx := event.C(inventory.Holder(c))
	_ = call(ctx, int(from.Slot), i.Grow(int(count)-i.Count()), invA.Handler().HandleTake)
	err := call(ctx, int(to.Slot), i.Grow(int(count)-i.Count()), invB.Handler().HandlePlace)
	if err != nil {
		return err
	}

	h.setItemInSlot(from, i.Grow(-int(count)), s, tx)
	h.setItemInSlot(to, dest.Grow(int(count)), s, tx)
	h.collectRewards(s, invA, int(from.Slot), tx, c)
	return nil
}

// handleSwap handles a Swap stack request action.
func (h *ItemStackRequestHandler) handleSwap(a *protocol.SwapStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	if err := h.verifySlots(s, tx, a.Source, a.Destination); err != nil {
		return fmt.Errorf("slot out of sync: %w", err)
	}
	i, _ := h.itemInSlot(a.Source, s, tx)
	dest, _ := h.itemInSlot(a.Destination, s, tx)

	invA, _ := s.invByID(int32(a.Source.Container.ContainerID), tx)
	invB, _ := s.invByID(int32(a.Destination.Container.ContainerID), tx)

	ctx := event.C(inventory.Holder(c))
	_ = call(ctx, int(a.Source.Slot), i, invA.Handler().HandleTake)
	_ = call(ctx, int(a.Source.Slot), dest, invA.Handler().HandlePlace)
	_ = call(ctx, int(a.Destination.Slot), dest, invB.Handler().HandleTake)
	err := call(ctx, int(a.Destination.Slot), i, invB.Handler().HandlePlace)
	if err != nil {
		return err
	}

	h.setItemInSlot(a.Source, dest, s, tx)
	h.setItemInSlot(a.Destination, i, s, tx)
	h.collectRewards(s, invA, int(a.Source.Slot), tx, c)
	h.collectRewards(s, invA, int(a.Destination.Slot), tx, c)
	return nil
}

// collectRewards checks if the source inventory has rewards for the player, for example, experience rewards when
// smelting. If it does, it will drop the rewards at the player's location.
// The furnace and the orbs are touched only once the action has succeeded.
func (h *ItemStackRequestHandler) collectRewards(s *Session, inv *inventory.Inventory, slot int, tx *world.Tx, c Controllable) {
	if inv == s.openedWindow.Load() && s.containerOpened.Load() && slot == inv.Size()-1 {
		if f, ok := tx.Block(*s.openedPos.Load()).(smelter); ok {
			h.effect(func() {
				// ResetExperience is called either way: the experience the furnace
				// holds is destroyed rather than kept for when the rule is unset.
				xp := f.ResetExperience()
				if !tx.World().ExperienceOrbsDisabled() {
					for _, o := range entity.NewExperienceOrbs(entity.EyePosition(c), xp) {
						tx.AddEntity(o)
					}
				}
			})
		}
	}
}

// handleDestroy handles the destroying of an item by moving it into the creative inventory.
func (h *ItemStackRequestHandler) handleDestroy(a *protocol.DestroyStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	if h.ignoreDestroy {
		return nil
	}
	if !c.GameMode().CreativeInventory() {
		return fmt.Errorf("can only destroy items in gamemode creative/spectator")
	}
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return fmt.Errorf("source slot out of sync: %w", err)
	}
	i, _ := h.itemInSlot(a.Source, s, tx)
	if i.Count() < int(a.Count) {
		return fmt.Errorf("client attempted to destroy %v items, but only %v present", a.Count, i.Count())
	}

	h.setItemInSlot(a.Source, i.Grow(-int(a.Count)), s, tx)
	return nil
}

// handleDrop handles the dropping of an item by moving it outside the inventory while having the
// inventory opened.
func (h *ItemStackRequestHandler) handleDrop(a *protocol.DropStackRequestAction, s *Session, tx *world.Tx, c Controllable) error {
	if err := h.verifySlot(a.Source, s, tx); err != nil {
		return fmt.Errorf("source slot out of sync: %w", err)
	}
	i, _ := h.itemInSlot(a.Source, s, tx)
	if i.Count() < int(a.Count) {
		return fmt.Errorf("client attempted to drop %v items, but only %v present", a.Count, i.Count())
	}

	inv, _ := s.invByID(int32(a.Source.Container.ContainerID), tx)
	if err := call(event.C(inventory.Holder(c)), int(a.Source.Slot), i.Grow(int(a.Count)-i.Count()), inv.Handler().HandleDrop); err != nil {
		return err
	}

	// The slot is written before the drop so that nothing in this action can
	// fail once the item exists in the world. A cancelled drop puts it back.
	h.setItemInSlot(a.Source, i.Grow(-int(a.Count)), s, tx)
	if h.writeErr != nil {
		return h.writeErr
	}
	if n := c.Drop(i.Grow(int(a.Count) - i.Count())); n == 0 {
		h.setItemInSlot(a.Source, i, s, tx)
		return nil
	}
	h.checkpointed = true
	return nil
}

// handleMineBlock handles the action associated with a block being mined by the player. This seems to be a workaround
// by Mojang to deal with the durability changes client-side.
func (h *ItemStackRequestHandler) handleMineBlock(a *protocol.MineBlockStackRequestAction, s *Session, tx *world.Tx) error {
	slot := protocol.StackRequestSlotInfo{
		Container:      protocol.FullContainerName{ContainerID: protocol.ContainerInventory},
		Slot:           byte(a.HotbarSlot),
		StackNetworkID: a.StackNetworkID,
	}
	if err := h.verifySlot(slot, s, tx); err != nil {
		return err
	}

	// Update the slots through ItemStackResponses, don't actually do anything special with this action.
	i, _ := h.itemInSlot(slot, s, tx)
	h.setItemInSlot(slot, i, s, tx)
	return nil
}

// handleCreate handles the CreateStackRequestAction sent by the client when a recipe outputs more than one item. It
// contains a result slot, which should map to one of the output items. From there, the server should create the relevant
// output as usual.
func (h *ItemStackRequestHandler) handleCreate(a *protocol.CreateStackRequestAction, s *Session, tx *world.Tx) error {
	slot := int(a.ResultsSlot)
	if slot >= len(h.pendingResults) {
		return fmt.Errorf("invalid pending result slot: %v", a.ResultsSlot)
	}

	res := h.pendingResults[slot]
	if res.Empty() {
		return fmt.Errorf("tried duplicating created result: %v", slot)
	}
	h.pendingResults[slot] = item.Stack{}

	h.setItemInSlot(protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: protocol.ContainerCreatedOutput},
		Slot:      craftingResult,
	}, res, s, tx)
	return nil
}

// defaultCreation represents the CreateStackRequestAction used for single-result crafts.
var defaultCreation = &protocol.CreateStackRequestAction{}

// createResults creates a new craft result and adds it to the list of pending craft results.
func (h *ItemStackRequestHandler) createResults(s *Session, tx *world.Tx, result ...item.Stack) error {
	h.pendingResults = append(h.pendingResults, result...)
	if len(result) > 1 {
		// With multiple results, the client notifies the server on when to create the results.
		return nil
	}
	return h.handleCreate(defaultCreation, s, tx)
}

// verifySlots verifies a list of slots passed.
func (h *ItemStackRequestHandler) verifySlots(s *Session, tx *world.Tx, slots ...protocol.StackRequestSlotInfo) error {
	for _, slot := range slots {
		if err := h.verifySlot(slot, s, tx); err != nil {
			return err
		}
	}
	return nil
}

// verifySlot checks if the slot passed by the client is the same as that expected by the server.
func (h *ItemStackRequestHandler) verifySlot(slot protocol.StackRequestSlotInfo, s *Session, tx *world.Tx) error {
	if err := h.tryAcknowledgeChanges(s, tx, slot); err != nil {
		return err
	}
	if len(h.responseChanges) > 256 {
		return fmt.Errorf("too many unacknowledged request slot changes")
	}
	i, err := h.itemInSlot(slot, s, tx)
	if err != nil {
		return err
	}
	// resolveID hands a non-negative stack network ID straight back without
	// looking at the inventory, and for an opened container invByID reads the
	// block at the container position to find it. The lookup is therefore only
	// done on the negative path, where it was already the only user of it.
	// Neither call has an effect, so doing it after itemInSlot changes nothing.
	clientID := slot.StackNetworkID
	if clientID < 0 {
		inv, _ := s.invByID(int32(slot.Container.ContainerID), tx)
		if clientID, err = h.resolveID(inv, slot); err != nil {
			return err
		}
	}
	// The client seems to send negative stack network IDs for predictions, which we can ignore. We'll simply
	// override this network ID later.
	if id := item_id(i); id != clientID {
		return fmt.Errorf("stack ID mismatch: client expected %v, but server had %v", clientID, id)
	}
	return nil
}

// resolveID resolves the stack network ID in the slot passed. If it is negative, it points to an earlier
// request, in which case it will look it up in the changes of an earlier response to a request to find the
// actual stack network ID in the slot. If it is positive, the ID will be returned again.
func (h *ItemStackRequestHandler) resolveID(inv *inventory.Inventory, slot protocol.StackRequestSlotInfo) (int32, error) {
	if slot.StackNetworkID >= 0 {
		return slot.StackNetworkID, nil
	}
	containerChanges, ok := h.responseChanges[slot.StackNetworkID]
	if !ok {
		return 0, fmt.Errorf("slot pointed to stack request %v, but request could not be found", slot.StackNetworkID)
	}
	changes, ok := containerChanges[inv]
	if !ok {
		return 0, fmt.Errorf("slot pointed to stack request %v with container %v, but that container was not changed in the request", slot.StackNetworkID, slot.Container.ContainerID)
	}
	actual, ok := changes[slot.Slot]
	if !ok {
		return 0, fmt.Errorf("slot pointed to stack request %v with container %v and slot %v, but that slot was not changed in the request", slot.StackNetworkID, slot.Container.ContainerID, slot.Slot)
	}
	return actual.id, nil
}

// tryAcknowledgeChanges iterates through all cached response changes and checks if the stack request slot
// info passed from the client has the right stack network ID in any of the stored slots. If this is the case,
// that entry is removed, so that the maps are cleaned up eventually.
func (h *ItemStackRequestHandler) tryAcknowledgeChanges(s *Session, tx *world.Tx, slot protocol.StackRequestSlotInfo) error {
	inv, ok := s.invByID(int32(slot.Container.ContainerID), tx)
	if !ok {
		return fmt.Errorf("could not find container with id %v", slot.Container.ContainerID)
	}

	for requestID, containerChanges := range h.responseChanges {
		for newInv, changes := range containerChanges {
			for slotIndex, val := range changes {
				if (slot.Slot == slotIndex && slot.StackNetworkID >= 0 && newInv == inv) || h.current.Sub(val.timestamp) > time.Second*5 {
					delete(changes, slotIndex)
				}
			}
			if len(changes) == 0 {
				delete(containerChanges, newInv)
			}
		}
		if len(containerChanges) == 0 {
			delete(h.responseChanges, requestID)
		}
	}
	return nil
}

// itemInSlot looks for the item in the slot as indicated by the slot info passed.
func (h *ItemStackRequestHandler) itemInSlot(slot protocol.StackRequestSlotInfo, s *Session, tx *world.Tx) (item.Stack, error) {
	inv, ok := s.invByID(int32(slot.Container.ContainerID), tx)
	if !ok {
		return item.Stack{}, fmt.Errorf("unable to find container with ID %v", slot.Container.ContainerID)
	}

	sl := int(slot.Slot)
	if inv == s.offHand {
		sl = 0
	}

	i, err := inv.Item(sl)
	if err != nil {
		return i, err
	}
	return i, nil
}

// setItemInSlot sets an item stack in the slot of a container present in the slot info.
func (h *ItemStackRequestHandler) setItemInSlot(slot protocol.StackRequestSlotInfo, i item.Stack, s *Session, tx *world.Tx) {
	inv, _ := s.invByID(int32(slot.Container.ContainerID), tx)

	sl := int(slot.Slot)
	if inv == s.offHand {
		sl = 0
	}

	before, _ := inv.Item(sl)
	h.recordOriginal(inv, sl, before)
	// The error is read back below instead: SetItem also returns nil when the
	// slot validator refused the item and nothing was written.
	_ = inv.SetItem(sl, i)
	// Which input was the problem: an item the slot does not accept, such as a
	// block moved into an armour slot. Nothing was written, yet the response
	// reported the item there, and a swap had already written the other slot,
	// so one item ended up in both. The action fails instead.
	if got, _ := inv.Item(sl); !got.Equal(i) && h.writeErr == nil {
		h.writeErr = fmt.Errorf("slot %v of container %v refused %v", slot.Slot, slot.Container.ContainerID, i)
	}

	respSlot := protocol.StackResponseSlotInfo{
		Slot:                 slot.Slot,
		HotbarSlot:           slot.Slot,
		Count:                byte(i.Count()),
		StackNetworkID:       item_id(i),
		DurabilityCorrection: int32(i.MaxDurability() - i.Durability()),
	}

	containerChanges := h.changes[slot.Container.ContainerID]
	if containerChanges == nil {
		containerChanges = map[byte]changeInfo{}
		h.changes[slot.Container.ContainerID] = containerChanges
	}
	containerChanges[slot.Slot] = changeInfo{
		after:  respSlot,
		before: before,
	}

	requestChanges := h.responseChanges[h.currentRequest]
	if requestChanges == nil {
		requestChanges = map[*inventory.Inventory]map[byte]responseChange{}
		h.responseChanges[h.currentRequest] = requestChanges
	}
	invChanges := requestChanges[inv]
	if invChanges == nil {
		invChanges = map[byte]responseChange{}
		requestChanges[inv] = invChanges
	}
	invChanges[slot.Slot] = responseChange{
		id:        respSlot.StackNetworkID,
		timestamp: h.current,
	}
}

// resolve resolves the request with the ID passed.
func (h *ItemStackRequestHandler) resolve(id int32, s *Session) {
	info := make([]protocol.StackResponseContainerInfo, 0, len(h.changes))
	for container, slotInfo := range h.changes {
		slots := make([]protocol.StackResponseSlotInfo, 0, len(slotInfo))
		for _, slot := range slotInfo {
			slots = append(slots, slot.after)
		}
		info = append(info, protocol.StackResponseContainerInfo{
			Container: protocol.FullContainerName{ContainerID: container},
			SlotInfo:  slots,
		})
	}
	s.writePacket(&packet.ItemStackResponse{Responses: []protocol.ItemStackResponse{{
		Status:        protocol.ItemStackResponseStatusOK,
		RequestID:     id,
		ContainerInfo: info,
	}}})

	// Emptying the map in place leaves the handler with the same empty map a
	// fresh one gave it: nothing outside the handler holds a reference to it,
	// and the packet above copied out every value it needed. The buckets are
	// reused instead of being allocated again for the next request.
	clear(h.changes)
	h.pendingResults = nil
	h.endRequest()
}

// reject rejects the item stack request sent by the client so that it is reverted client-side.
func (h *ItemStackRequestHandler) reject(id int32, s *Session, tx *world.Tx) {
	s.writePacket(&packet.ItemStackResponse{
		Responses: []protocol.ItemStackResponse{{
			Status:    protocol.ItemStackResponseStatusError,
			RequestID: id,
		}},
	})

	// Revert the changes made since the last checkpoint. Which input was the
	// problem: a request writing the same slot twice before failing, for
	// example moving A to B and then B to C. The value before the last write
	// was restored rather than the one before the first, so B kept the stack
	// A had given it while A got it back. Only the first write of a slot is
	// recorded, and the revert writes to the inventory recorded rather than
	// looking the container up again: the container may be gone by now, as an
	// anvil broken by the request is, and the offhand needs slot 0.
	for _, o := range h.originals {
		// The slot was written moments ago in this same inventory, so it is in
		// range and an error cannot occur.
		_ = o.inv.SetItem(o.slot, o.before)
	}
	// The client reverts the whole request, including the part a checkpoint
	// made final, so it is told what those inventories really hold.
	for _, inv := range h.committed {
		s.resendInventory(inv)
	}
	// The client never refers to a request that was rejected.
	delete(h.responseChanges, id)

	// Same as in resolve: the map is emptied in place rather than replaced, so
	// the handler is left holding an empty map either way.
	clear(h.changes)
	h.pendingResults = nil
	// Which input was the problem: a beacon payment followed by a failing
	// action. The flag was only reset on success, so every Destroy in the
	// requests after it was ignored instead of checked.
	h.ignoreDestroy = false
	h.endRequest()
}

// endRequest drops the state kept for the request that just ended.
func (h *ItemStackRequestHandler) endRequest() {
	clear(h.originals)
	h.originals = h.originals[:0]
	clear(h.committed)
	h.committed = h.committed[:0]
	h.checkpointed = false
	h.writeErr = nil
}

// recordOriginal records before as the contents of slot in inv, unless the
// slot was already written since the start of the request or its last
// checkpoint.
func (h *ItemStackRequestHandler) recordOriginal(inv *inventory.Inventory, slot int, before item.Stack) {
	for _, o := range h.originals {
		if o.inv == inv && o.slot == slot {
			return
		}
	}
	h.originals = append(h.originals, slotOriginal{inv: inv, slot: slot, before: before})
}

// expireResponseChanges removes the response changes older than five seconds,
// which the client will have received by then.
func (h *ItemStackRequestHandler) expireResponseChanges() {
	for requestID, containerChanges := range h.responseChanges {
		for inv, changes := range containerChanges {
			for slot, val := range changes {
				if h.current.Sub(val.timestamp) > time.Second*5 {
					delete(changes, slot)
				}
			}
			if len(changes) == 0 {
				delete(containerChanges, inv)
			}
		}
		if len(containerChanges) == 0 {
			delete(h.responseChanges, requestID)
		}
	}
}

// call uses an event.Context, slot and item.Stack to call the event handler function passed. An error is returned if
// the event.Context was cancelled either before or after the call.
func call(ctx *inventory.Context, slot int, it item.Stack, f func(ctx *inventory.Context, slot int, it item.Stack)) error {
	if ctx.Cancelled() {
		return fmt.Errorf("action was cancelled")
	}
	f(ctx, slot, it)
	if ctx.Cancelled() {
		return fmt.Errorf("action was cancelled")
	}
	return nil
}
