package session

import (
	"encoding/json"
	"fmt"
	"image/color"
	"maps"
	"math"
	"net"
	"slices"
	"time"
	_ "unsafe" // Imported for compiler directives.

	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/entity"
	"github.com/jv2w/Impulse/server/entity/effect"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/creative"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/item/recipe"
	"github.com/jv2w/Impulse/server/player/debug"
	"github.com/jv2w/Impulse/server/player/dialogue"
	"github.com/jv2w/Impulse/server/player/form"
	"github.com/jv2w/Impulse/server/player/hud"
	"github.com/jv2w/Impulse/server/player/input"
	"github.com/jv2w/Impulse/server/player/skin"
	"github.com/jv2w/Impulse/server/world"
	"github.com/jv2w/Impulse/server/world/sound"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// StopShowingEntity stops showing a world.Entity to the Session. It will be completely invisible until a call to
// StartShowingEntity is made.
func (s *Session) StopShowingEntity(e world.Entity) {
	s.entityMutex.Lock()
	id := e.H().UUID()
	_, ok := s.hiddenEntities[id]
	if !ok {
		s.hiddenEntities[id] = struct{}{}
	}
	s.entityMutex.Unlock()

	if !ok {
		s.HideEntity(e)
	}
}

// StartShowingEntity starts showing a world.Entity to the Session that was previously hidden using StopShowingEntity.
func (s *Session) StartShowingEntity(e world.Entity) {
	s.entityMutex.Lock()
	id := e.H().UUID()
	_, ok := s.hiddenEntities[id]
	if ok {
		delete(s.hiddenEntities, id)
	}
	s.entityMutex.Unlock()

	if ok {
		s.ViewEntity(e)
		s.ViewEntityState(e)
		s.ViewEntityItems(e)
		s.ViewEntityArmour(e)
	}
}

// closeCurrentContainer closes the container the player might currently have open.
func (s *Session) closeCurrentContainer(tx *world.Tx, clientRequested bool) {
	if !s.closeWindow(clientRequested) {
		return
	}

	pos := *s.openedPos.Load()
	b := tx.Block(pos)
	if container, ok := b.(block.Container); ok {
		container.RemoveViewer(s, tx, pos)
	} else if enderChest, ok := b.(block.EnderChest); ok {
		enderChest.RemoveViewer(tx, pos)
	}
}

// SendRespawn spawns the Controllable entity of the session client-side in the world, provided it has died.
func (s *Session) SendRespawn(pos mgl64.Vec3, c Controllable) {
	s.writePacket(&packet.Respawn{
		Position:        vec64To32(pos.Add(entityOffset(c))),
		State:           packet.RespawnStateReadyToSpawn,
		EntityRuntimeID: selfEntityRuntimeID,
	})
}

// SendPlayerSpawn updates the player's spawn point on the client-side. There is currently little reason
// to do so other than to prevent the client-side "Respawn point set" message when sleeping in a bed.
func (s *Session) SendPlayerSpawn(pos mgl64.Vec3) {
	blockPos := protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&packet.SetSpawnPosition{
		SpawnType:     packet.SpawnTypePlayer,
		Position:      blockPos,
		Dimension:     packet.DimensionOverworld,
		SpawnPosition: blockPos,
	})
}

// sendBiomes sends all the vanilla biomes to the session.
func (s *Session) sendBiomes() {
	definitions, stringList := world.BiomeDefinitions()
	s.writePacket(&packet.BiomeDefinitionList{
		BiomeDefinitions: definitions,
		StringList:       stringList,
	})
}

// sendRecipes sends the current crafting recipes to the session.
func (s *Session) sendRecipes() {
	var (
		shapedRecipes            []protocol.ShapedRecipe
		shapelessRecipes         []protocol.ShapelessRecipe
		userDataShapelessRecipes []protocol.UserDataShapelessRecipe
		multiRecipes             []protocol.MultiRecipe
		smithingTransformRecipes []protocol.SmithingTransformRecipe
		smithingTrimRecipes      []protocol.SmithingTrimRecipe
		potionRecipes            []protocol.PotionRecipe
		potionContainerChange    []protocol.PotionContainerChangeRecipe
	)

	for index, i := range recipe.Recipes() {
		networkID := uint32(index) + 1
		s.recipes[networkID] = i

		switch i := i.(type) {
		case recipe.Shapeless:
			shapelessRecipes = append(shapelessRecipes, protocol.ShapelessRecipe{
				RecipeID:        uuid.New().String(),
				Priority:        int32(i.Priority()),
				Input:           stacksToIngredientItems(s.br, i.Input()),
				Output:          stacksToRecipeStacks(s.br, i.Output()),
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.UserDataShapeless:
			userDataShapelessRecipes = append(userDataShapelessRecipes, protocol.UserDataShapelessRecipe{ShapelessRecipe: protocol.ShapelessRecipe{
				RecipeID:        uuid.New().String(),
				Priority:        int32(i.Priority()),
				Input:           stacksToIngredientItems(s.br, i.Input()),
				Output:          stacksToRecipeStacks(s.br, i.Output()),
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			}})
		case recipe.Multi:
			multiRecipes = append(multiRecipes, protocol.MultiRecipe{
				UUID:            i.UUID(),
				RecipeNetworkID: networkID,
			})
		case recipe.Shaped:
			// The shape is read once instead of twice; it is a stored value with no side effect, so the
			// width and height below come from the same shape as before.
			shape := i.Shape()
			shapedRecipes = append(shapedRecipes, protocol.ShapedRecipe{
				RecipeID:        uuid.New().String(),
				Priority:        int32(i.Priority()),
				Width:           int32(shape.Width()),
				Height:          int32(shape.Height()),
				Input:           stacksToIngredientItems(s.br, i.Input()),
				Output:          stacksToRecipeStacks(s.br, i.Output()),
				Block:           i.Block(),
				AssumeSymmetry:  true,
				RecipeNetworkID: networkID,
			})
		case recipe.SmithingTransform:
			input, output := stacksToIngredientItems(s.br, i.Input()), stacksToRecipeStacks(s.br, i.Output())
			smithingTransformRecipes = append(smithingTransformRecipes, protocol.SmithingTransformRecipe{
				RecipeID:        uuid.New().String(),
				Base:            input[0],
				Addition:        input[1],
				Template:        input[2],
				Result:          output[0],
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.SmithingTrim:
			input := stacksToIngredientItems(s.br, i.Input())
			smithingTrimRecipes = append(smithingTrimRecipes, protocol.SmithingTrimRecipe{
				RecipeID:        uuid.New().String(),
				Base:            input[0],
				Addition:        input[1],
				Template:        input[2],
				Block:           i.Block(),
				RecipeNetworkID: networkID,
			})
		case recipe.Potion:
			// Input returns the stored slice, so reading it once names the same two ingredients.
			potionInput := i.Input()
			inputRuntimeID, inputMeta, _ := world.ItemRuntimeID(potionInput[0].(item.Stack).Item())
			reagentRuntimeID, reagentMeta, _ := world.ItemRuntimeID(potionInput[1].(item.Stack).Item())
			outputRuntimeID, outputMeta, _ := world.ItemRuntimeID(i.Output()[0].Item())

			potionRecipes = append(potionRecipes, protocol.PotionRecipe{
				InputPotionID:        inputRuntimeID,
				InputPotionMetadata:  int32(inputMeta),
				ReagentItemID:        reagentRuntimeID,
				ReagentItemMetadata:  int32(reagentMeta),
				OutputPotionID:       outputRuntimeID,
				OutputPotionMetadata: int32(outputMeta),
			})

		case recipe.PotionContainerChange:
			// Input returns the stored slice, so reading it once names the same two ingredients.
			containerInput := i.Input()
			inputRuntimeID, _, _ := world.ItemRuntimeID(containerInput[0].(item.Stack).Item())
			reagentRuntimeID, _, _ := world.ItemRuntimeID(containerInput[1].(item.Stack).Item())
			outputRuntimeID, _, _ := world.ItemRuntimeID(i.Output()[0].Item())

			potionContainerChange = append(potionContainerChange, protocol.PotionContainerChangeRecipe{
				InputItemID:   inputRuntimeID,
				ReagentItemID: reagentRuntimeID,
				OutputItemID:  outputRuntimeID,
			})
		}
	}
	s.writePacket(&packet.CraftingData{
		ShapedRecipes:                shapedRecipes,
		ShapelessRecipes:             shapelessRecipes,
		MultiRecipes:                 multiRecipes,
		UserDataShapelessRecipes:     userDataShapelessRecipes,
		SmithingTransformRecipes:     smithingTransformRecipes,
		SmithingTrimRecipes:          smithingTrimRecipes,
		PotionRecipes:                potionRecipes,
		PotionContainerChangeRecipes: potionContainerChange,
		ClearRecipes:                 true,
	})
}

// sendArmourTrimData sends the armour trim data.
func (s *Session) sendArmourTrimData() {
	// Both lists hold at most one entry per registered value, so they are allocated once up front instead of
	// being grown. They only ever reach the packet writer, which writes a count of zero for an empty slice
	// just as it did for a nil one.
	templates, materials := item.SmithingTemplates(), item.ArmourTrimMaterials()
	trimPatterns := make([]protocol.TrimPattern, 0, len(templates))
	trimMaterials := make([]protocol.TrimMaterial, 0, len(materials))

	// The netherite upgrade template is a constant value, so it is built once instead of once per template.
	netheriteUpgrade := item.TemplateNetheriteUpgrade()
	for _, t := range templates {
		if t == netheriteUpgrade {
			continue
		}
		name, _ := item.SmithingTemplate{Template: t}.EncodeItem()
		trimPatterns = append(trimPatterns, protocol.TrimPattern{
			ItemName:  name,
			PatternID: t.String(),
		})
	}

	for _, i := range materials {
		if material, ok := i.(item.ArmourTrimMaterial); ok {
			name, _ := i.EncodeItem()

			trimMaterials = append(trimMaterials, protocol.TrimMaterial{
				MaterialID: material.TrimMaterial(),
				Colour:     material.MaterialColour(),
				ItemName:   name,
			})
		}
	}

	s.writePacket(&packet.TrimData{Patterns: trimPatterns, Materials: trimMaterials})
}

// sendInv sends the inventory passed to the client with the window ID.
func (s *Session) sendInv(inv *inventory.Inventory, windowID uint32) {
	pk := &packet.InventoryContent{
		WindowID: windowID,
		Content:  make([]protocol.ItemInstance, 0, inv.Size()),
	}
	for _, i := range inv.Slots() {
		pk.Content = append(pk.Content, instanceFromItem(s.br, i))
	}
	s.writePacket(pk)
}

// sendItem sends the item stack passed to the client with the window ID and slot passed.
func (s *Session) sendItem(item item.Stack, slot int, windowID uint32) {
	s.writePacket(&packet.InventorySlot{
		WindowID: windowID,
		Slot:     uint32(slot),
		NewItem:  instanceFromItem(s.br, item),
	})
}

const (
	craftingGridSizeSmall   = 4
	craftingGridSizeLarge   = 9
	craftingGridSmallOffset = 28
	craftingGridLargeOffset = 32
	craftingResult          = 50
)

// smelter is an interface representing a block used to smelt items.
type smelter interface {
	// ResetExperience resets the collected experience of the smelter, and returns the amount of experience that was reset.
	ResetExperience() int
}

// invByID attempts to return an inventory by the ID passed. If found, the inventory is returned and the bool
// returned is true.
func (s *Session) invByID(id int32, tx *world.Tx) (*inventory.Inventory, bool) {
	switch id {
	case protocol.ContainerCraftingInput, protocol.ContainerCreatedOutput, protocol.ContainerCursor:
		// UI inventory.
		return s.ui, true
	case protocol.ContainerHotBar, protocol.ContainerInventory, protocol.ContainerCombinedHotBarAndInventory:
		// Hotbar 'inventory', rest of inventory, inventory when container is opened.
		return s.inv, true
	case protocol.ContainerOffhand:
		return s.offHand, true
	case protocol.ContainerArmor:
		// Armour inventory.
		return s.armour.Inventory(), true
	default:
		if !s.containerOpened.Load() {
			return nil, false
		}
		switch id {
		case protocol.ContainerLevelEntity:
			return s.openedWindow.Load(), true
		case protocol.ContainerShulkerBox:
			if _, shulkerbox := tx.Block(*s.openedPos.Load()).(block.ShulkerBox); shulkerbox {
				return s.openedWindow.Load(), true
			}
		case protocol.ContainerBarrel:
			if _, barrel := tx.Block(*s.openedPos.Load()).(block.Barrel); barrel {
				return s.openedWindow.Load(), true
			}
		case protocol.ContainerBeaconPayment:
			if _, beacon := tx.Block(*s.openedPos.Load()).(block.Beacon); beacon {
				return s.ui, true
			}
		case protocol.ContainerBrewingStandInput, protocol.ContainerBrewingStandResult, protocol.ContainerBrewingStandFuel:
			if _, brewingStand := tx.Block(*s.openedPos.Load()).(block.BrewingStand); brewingStand {
				return s.openedWindow.Load(), true
			}
		case protocol.ContainerAnvilInput, protocol.ContainerAnvilMaterial:
			if _, anvil := tx.Block(*s.openedPos.Load()).(block.Anvil); anvil {
				return s.ui, true
			}
		case protocol.ContainerSmithingTableTemplate, protocol.ContainerSmithingTableInput, protocol.ContainerSmithingTableMaterial:
			if _, smithing := tx.Block(*s.openedPos.Load()).(block.SmithingTable); smithing {
				return s.ui, true
			}
		case protocol.ContainerLoomInput, protocol.ContainerLoomDye, protocol.ContainerLoomMaterial:
			if _, loom := tx.Block(*s.openedPos.Load()).(block.Loom); loom {
				return s.ui, true
			}
		case protocol.ContainerStonecutterInput:
			if _, ok := tx.Block(*s.openedPos.Load()).(block.Stonecutter); ok {
				return s.ui, true
			}
		case protocol.ContainerGrindstoneInput, protocol.ContainerGrindstoneAdditional:
			if _, ok := tx.Block(*s.openedPos.Load()).(block.Grindstone); ok {
				return s.ui, true
			}
		case protocol.ContainerEnchantingInput, protocol.ContainerEnchantingMaterial:
			if _, enchanting := tx.Block(*s.openedPos.Load()).(block.EnchantingTable); enchanting {
				return s.ui, true
			}
		case protocol.ContainerFurnaceIngredient, protocol.ContainerFurnaceFuel, protocol.ContainerFurnaceResult,
			protocol.ContainerBlastFurnaceIngredient, protocol.ContainerSmokerIngredient:
			if _, ok := tx.Block(*s.openedPos.Load()).(smelter); ok {
				return s.openedWindow.Load(), true
			}
		}
	}
	return nil, false
}

// Disconnect disconnects the client and ultimately closes the session. If the message passed is non-empty,
// it will be shown to the client.
func (s *Session) Disconnect(message string) {
	if s != Nop {
		_ = s.conn.WritePacket(&packet.Disconnect{
			HideDisconnectionScreen: message == "",
			Message:                 message,
		})
		_ = s.conn.Flush()
	}
}

// SendSpeed sends the speed of the player in an UpdateAttributes packet, so that it is updated client-side.
func (s *Session) SendSpeed(speed float64) {
	s.writePacket(&packet.UpdateAttributes{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []protocol.Attribute{{
			AttributeValue: protocol.AttributeValue{
				Name:  "minecraft:movement",
				Value: float32(speed),
				Max:   math.MaxFloat32,
			},
			DefaultMax: math.MaxFloat32,
			Default:    0.1,
		}},
	})
}

// SendFood ...
func (s *Session) SendFood(food int, saturation, exhaustion float64) {
	s.writePacket(&packet.UpdateAttributes{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []protocol.Attribute{
			{
				AttributeValue: protocol.AttributeValue{
					Name:  "minecraft:player.hunger",
					Value: float32(food),
					Max:   20,
				},
				DefaultMax: 20,
				Default:    20,
			},
			{
				AttributeValue: protocol.AttributeValue{
					Name:  "minecraft:player.saturation",
					Value: float32(saturation),
					Max:   20,
				},
				DefaultMax: 20,
				Default:    20,
			},
			{
				AttributeValue: protocol.AttributeValue{
					Name:  "minecraft:player.exhaustion",
					Value: float32(exhaustion),
					Max:   5,
				},
				DefaultMax: 5,
			},
		},
	})
}

// SendDialogue sends an NPC dialogue to the client of the connection. The Submit method of the dialogue is
// called when the client interacts with a button in the dialogue.
func (s *Session) SendDialogue(d dialogue.Dialogue, e world.Entity) {
	b, _ := json.Marshal(d)

	h := s.handlers[packet.IDNPCRequest].(*NPCRequestHandler)
	h.dialogue = d
	h.entityRuntimeID = s.entityRuntimeID(e)

	metadata := s.parseEntityMetadata(e)
	metadata[protocol.EntityDataKeyHasNPC] = uint8(1)

	disp := d.Display()
	disp.EntityOffset = disp.EntityOffset.Add(entityOffset(e))
	display, _ := json.Marshal(map[string]any{"portrait_offsets": disp})
	metadata[protocol.EntityDataKeyNPCData] = string(display)

	s.writePacket(&packet.SetActorData{
		EntityRuntimeID: h.entityRuntimeID,
		EntityMetadata:  metadata,
	})
	s.writePacket(&packet.NPCDialogue{
		EntityUniqueID: h.entityRuntimeID,
		ActionType:     packet.NPCDialogueActionOpen,
		Dialogue:       d.Body(),
		SceneName:      "default",
		NPCName:        d.Title(),
		ActionJSON:     string(b),
	})
}

func (s *Session) CloseDialogue() {
	h := s.handlers[packet.IDNPCRequest].(*NPCRequestHandler)
	if h.entityRuntimeID == 0 {
		return
	}

	s.writePacket(&packet.NPCDialogue{
		EntityUniqueID: h.entityRuntimeID,
		ActionType:     packet.NPCDialogueActionClose,
	})
	h.entityRuntimeID = 0
}

// SendForm sends a form to the client of the connection. The Submit method of the form is called when the
// client submits the form.
func (s *Session) SendForm(f form.Form) {
	b, _ := json.Marshal(f)

	h := s.handlers[packet.IDModalFormResponse].(*ModalFormResponseHandler)
	id := h.currentID.Add(1)

	h.mu.Lock()
	if len(h.forms) > 10 {
		s.conf.Log.Debug("SendForm: more than 10 active forms: dropping an existing one")
		for k := range h.forms {
			delete(h.forms, k)
			break
		}
	}
	h.forms[id] = f
	h.mu.Unlock()

	s.writePacket(&packet.ModalFormRequest{
		FormID:   id,
		FormData: b,
	})
}

// CloseForm closes any forms that the player currently has open. If the player has no forms open, nothing
// happens.
func (s *Session) CloseForm() {
	s.writePacket(&packet.ClientBoundCloseForm{})
}

// Transfer transfers the player to a server with the IP and port passed.
func (s *Session) Transfer(ip net.IP, port int) {
	s.writePacket(&packet.Transfer{
		Address: ip.String(),
		Port:    uint16(port),
	})
}

// SendGameMode sends the game mode of the Controllable entity of the session to the client. It makes sure the right
// flags are set to create the full game mode.
func (s *Session) SendGameMode(c Controllable) {
	if s == Nop {
		return
	}
	s.writePacket(&packet.SetPlayerGameType{GameType: gameTypeFromMode(c.GameMode())})
	s.SendAbilities(c)
}

// SendAbilities sends the abilities of the Controllable entity of the session to the client.
func (s *Session) SendAbilities(c Controllable) {
	mode, abilities := c.GameMode(), uint32(0)
	if mode.AllowsFlying() {
		abilities |= protocol.AbilityMayFly
		if c.Flying() {
			abilities |= protocol.AbilityFlying
		}
	}
	if !mode.HasCollision() {
		abilities |= protocol.AbilityNoClip
		defer c.StartFlying()
		// If the client is currently on the ground and turned to spectator mode, it will be unable to sprint during
		// flight. In order to allow this, we force the client to be flying through a MovePlayer packet.
		s.ViewEntityTeleport(c, c.Position())
	}
	if !mode.AllowsTakingDamage() {
		abilities |= protocol.AbilityInvulnerable
	}
	if mode.CreativeInventory() {
		abilities |= protocol.AbilityInstantBuild
	}
	if mode.AllowsEditing() {
		abilities |= protocol.AbilityBuild | protocol.AbilityMine
	}
	if mode.AllowsInteraction() {
		abilities |= protocol.AbilityDoorsAndSwitches | protocol.AbilityOpenContainers | protocol.AbilityAttackPlayers | protocol.AbilityAttackMobs
	}
	s.writePacket(&packet.UpdateAbilities{AbilityData: protocol.AbilityData{
		EntityUniqueID:     selfEntityRuntimeID,
		PlayerPermissions:  packet.PermissionLevelMember,
		CommandPermissions: protocol.CommandPermissionLevelAny,
		Layers: []protocol.AbilityLayer{
			{
				Type:             protocol.AbilityLayerTypeBase,
				Abilities:        protocol.AbilityCount - 1,
				Values:           abilities,
				FlySpeed:         float32(c.FlightSpeed()),
				VerticalFlySpeed: float32(c.VerticalFlightSpeed()),
				WalkSpeed:        protocol.AbilityBaseWalkSpeed,
			},
		},
	}})
}

// SendHealth sends the health and max health to the player.
func (s *Session) SendHealth(health, max, absorption float64) {
	s.writePacket(&packet.UpdateAttributes{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []protocol.Attribute{{
			AttributeValue: protocol.AttributeValue{
				Name:  "minecraft:health",
				Value: float32(math.Ceil(health)),
				Max:   float32(math.Ceil(max)),
			},
			DefaultMax: 20,
			Default:    20,
		}, {
			AttributeValue: protocol.AttributeValue{
				Name:  "minecraft:absorption",
				Value: float32(math.Ceil(absorption)),
				Max:   float32(math.MaxFloat32),
			},
			DefaultMax: float32(math.MaxFloat32),
		}},
	})
}

// SendEffect sends an effects passed to the player.
func (s *Session) SendEffect(e effect.Effect) {
	// The effect type is read once instead of twice. Sending the removal does not change the effect value,
	// so the ID looked up below belongs to the same type as before.
	t := e.Type()
	s.SendEffectRemoval(t)
	id, _ := effect.ID(t)
	dur := e.Duration() / (time.Second / 20)
	if e.Infinite() {
		dur = -1
	}
	s.writePacket(&packet.MobEffect{
		EntityRuntimeID: selfEntityRuntimeID,
		Operation:       packet.MobEffectAdd,
		EffectType:      int32(id),
		Amplifier:       int32(e.Level() - 1),
		Particles:       !e.ParticlesHidden(),
		Duration:        int32(dur),
		Ambient:         e.Ambient(),
	})
}

// SendEffectRemoval sends the removal of an effect passed.
func (s *Session) SendEffectRemoval(e effect.Type) {
	id, ok := effect.ID(e)
	if !ok {
		panic(fmt.Sprintf("unregistered effect type %T", e))
	}
	s.writePacket(&packet.MobEffect{
		EntityRuntimeID: selfEntityRuntimeID,
		Operation:       packet.MobEffectRemove,
		EffectType:      int32(id),
	})
}

// SendGameRules sends all the provided game rules to the player. Once sent, they will be immediately updated
// on the client if they are valid.
func (s *Session) sendGameRules(gameRules []protocol.GameRule) {
	s.writePacket(&packet.GameRulesChanged{GameRules: gameRules})
}

// EnableCoordinates will either enable or disable coordinates for the player depending on the value given.
func (s *Session) EnableCoordinates(enable bool) {
	//noinspection SpellCheckingInspection
	s.sendGameRules([]protocol.GameRule{{Name: "showcoordinates", Value: enable}})
}

// EnableInstantRespawn will either enable or disable instant respawn for the player depending on the value given.
func (s *Session) EnableInstantRespawn(enable bool) {
	//noinspection SpellCheckingInspection
	s.sendGameRules([]protocol.GameRule{{Name: "doimmediaterespawn", Value: enable}})
}

// HandleInventories starts handling the inventories of the Controllable entity of the session. It sends packets when
// slots in the inventory are changed.
func (s *Session) HandleInventories(tx *world.Tx, c Controllable, inv, offHand, enderChest, ui *inventory.Inventory, armour *inventory.Armour, heldSlot *uint32) {
	s.inv = inv
	s.inv.SlotFunc(s.broadcastInvFunc(tx, c))
	s.offHand = offHand
	s.offHand.SlotFunc(s.broadcastOffHandFunc(tx, c))
	s.enderChest = enderChest
	s.enderChest.SlotFunc(s.broadcastEnderChestFunc(tx, c))
	s.armour = armour
	s.armour.Inventory().SlotFunc(s.broadcastArmourFunc(tx, c))
	s.ui = ui
	s.ui.SlotFunc(s.uiInventoryFunc(tx, c))
	s.heldSlot = heldSlot
}

func (s *Session) broadcastInvFunc(tx *world.Tx, c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		if slot == int(*s.heldSlot) {
			for _, viewer := range tx.Viewers(c.Position()) {
				viewer.ViewEntityItems(c)
			}
		}
		if !s.inTransaction.Load() {
			s.sendItem(after, slot, protocol.WindowIDInventory)
		}
	}
}

func (s *Session) broadcastEnderChestFunc(tx *world.Tx, _ Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		if !s.inTransaction.Load() {
			if _, ok := tx.Block(*s.openedPos.Load()).(block.EnderChest); ok {
				s.ViewSlotChange(slot, after)
			}
		}
	}
}

func (s *Session) broadcastOffHandFunc(tx *world.Tx, c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		for _, viewer := range tx.Viewers(c.Position()) {
			viewer.ViewEntityItems(c)
		}
		if !s.inTransaction.Load() {
			i, _ := s.offHand.Item(0)
			s.writePacket(&packet.InventoryContent{
				WindowID: protocol.WindowIDOffHand,
				Content:  []protocol.ItemInstance{instanceFromItem(s.br, i)},
			})
		}
	}
}

func (s *Session) broadcastArmourFunc(tx *world.Tx, c Controllable) inventory.SlotFunc {
	return func(slot int, before, after item.Stack) {
		inTransaction := s.inTransaction.Load()
		if !inTransaction {
			s.sendItem(after, slot, protocol.WindowIDArmour)
		}
		if before.Comparable(after) && before.Empty() == after.Empty() {
			// Only send armour if the item type actually changed.
			return
		}
		for _, viewer := range tx.Viewers(c.Position()) {
			viewer.ViewEntityArmour(c)
		}

		if !after.Empty() && inTransaction {
			tx.PlaySound(entity.EyePosition(c), sound.EquipItem{Item: after.Item()})
		}
	}
}

// uiInventoryFunc handles an update to the UI inventory, used for updating enchantment options and possibly more
// in the future.
func (s *Session) uiInventoryFunc(tx *world.Tx, c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		if slot == enchantingInputSlot && s.containerOpened.Load() {
			pos := *s.openedPos.Load()
			if _, enchanting := tx.Block(pos).(block.EnchantingTable); enchanting {
				s.sendEnchantmentOptions(tx, c, pos, after)
			}
		}
		s.sendInv(s.ui, protocol.WindowIDUI)
	}
}

// SendHeldSlot sends the currently held hotbar slot.
func (s *Session) SendHeldSlot(slot int, c Controllable, force bool) {
	if s.changingSlot.Load() && !force {
		return
	}
	mainHand, _ := c.HeldItems()
	s.writePacket(&packet.MobEquipment{
		EntityRuntimeID: selfEntityRuntimeID,
		NewItem:         instanceFromItem(s.br, mainHand),
		InventorySlot:   byte(slot),
		HotBarSlot:      byte(slot),
	})
}

// VerifyAndSetHeldSlot verifies if the slot passed is a valid hotbar slot and
// if the expected item.Stack is in it. Afterwards, it changes the held slot
// of the player.
func (s *Session) VerifyAndSetHeldSlot(slot int, expected item.Stack, c Controllable) error {
	if err := s.VerifySlot(slot, expected); err != nil {
		return err
	}
	s.changingSlot.Store(true)
	defer s.changingSlot.Store(false)
	return c.SetHeldSlot(slot)
}

// VerifySlot verifies if the slot passed is a valid hotbar slot and if the
// expected item.Stack is in it.
func (s *Session) VerifySlot(slot int, expected item.Stack) error {
	// The slot that the player might have selected must be within the hotbar:
	// The held item cannot be in a different place in the inventory.
	if slot < 0 || slot > 8 {
		return fmt.Errorf("slot exceeds hotbar range 0-8: slot is %v", slot)
	}
	clientSideItem := expected
	actual, _ := s.inv.Item(slot)

	// The item the client claims to have must be identical to the one we have
	// registered server-side.
	if !clientSideItem.Equal(actual) {
		s.sendItem(actual, slot, protocol.WindowIDInventory)
		// Only ever debug these as they are frequent and expected to happen
		// whenever client and server get out of sync.
		s.conf.Log.Debug("verify slot: client-side item was not equal to server-side item", "client-held", clientSideItem.String(), "server-held", actual.String())
	}
	return nil
}

// SendExperience sends the experience level and progress from the given experience manager to the player.
func (s *Session) SendExperience(level int, progress float64) {
	s.writePacket(&packet.UpdateAttributes{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []protocol.Attribute{
			{
				AttributeValue: protocol.AttributeValue{
					Name:  "minecraft:player.level",
					Value: float32(level),
					Max:   float32(math.MaxInt32),
				},
				DefaultMax: float32(math.MaxInt32),
			},
			{
				AttributeValue: protocol.AttributeValue{
					Name:  "minecraft:player.experience",
					Value: float32(progress),
					Max:   1,
				},
				DefaultMax: 1,
			},
		},
	})
}

// SendChargeItemComplete sends a packet to indicate that the item charging process has been completed.
func (s *Session) SendChargeItemComplete() {
	s.writePacket(&packet.ActorEvent{
		EntityRuntimeID: selfEntityRuntimeID,
		EventType:       packet.ActorEventFinishedChargingItem,
	})
}

// ShowHudElement shows a HUD element to the player if it is not already shown. If the element is waiting to
// be hidden, it will be removed from the updates and remain visible to the player.
func (s *Session) ShowHudElement(e hud.Element) {
	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	if _, ok := s.hiddenHud[e]; ok {
		s.hudUpdates[e] = true
	} else if _, ok = s.hudUpdates[e]; ok {
		delete(s.hudUpdates, e)
	}
}

// HideHudElement hides a HUD element from the player if it is not already hidden. If the element is waiting
// to be shown, it will be removed from the updates and remain hidden from the player.
func (s *Session) HideHudElement(e hud.Element) {
	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	if _, ok := s.hiddenHud[e]; !ok {
		s.hudUpdates[e] = false
	} else if _, ok = s.hudUpdates[e]; ok {
		delete(s.hudUpdates, e)
	}
}

// HudElementHidden checks if a HUD element is currently hidden from the player.
func (s *Session) HudElementHidden(e hud.Element) bool {
	// The critical section only reads two maps, which cannot panic even when they are nil, so the unlock
	// does not need a defer frame. Both exits still unlock before returning.
	s.hudMu.RLock()
	if _, ok := s.hiddenHud[e]; ok {
		s.hudMu.RUnlock()
		return true
	}
	vis, ok := s.hudUpdates[e]
	s.hudMu.RUnlock()
	return ok && !vis
}

// SendHudUpdates sends any pending HUD updates to the player. The updates are batched to reduce the number
// of packets being sent. Up to 2 packets will be sent, one for showing elements and one for hiding elements.
func (s *Session) SendHudUpdates() {
	s.hudMu.Lock()
	if len(s.hudUpdates) == 0 {
		s.hudMu.Unlock()
		return
	}
	var show, hide []int32
	for e, vis := range s.hudUpdates {
		if vis {
			show = append(show, int32(e.Uint8()))
			delete(s.hiddenHud, e)
		} else {
			hide = append(hide, int32(e.Uint8()))
			s.hiddenHud[e] = struct{}{}
		}
	}
	s.hudUpdates = make(map[hud.Element]bool)
	s.hudMu.Unlock()

	if len(show) > 0 {
		s.writePacket(&packet.SetHud{Elements: show, Visibility: packet.HudVisibilityReset})
	}
	if len(hide) > 0 {
		s.writePacket(&packet.SetHud{Elements: hide, Visibility: packet.HudVisibilityHide})
	}
}

// AddDebugShape adds a debug shape to be rendered to the player. If the shape already exists, it will be
// updated with the new information.
func (s *Session) AddDebugShape(shape debug.Shape) {
	if s == Nop {
		return
	}
	s.queueDebugShapeUpdate(debugShapeUpdate{id: shape.ShapeID(), shape: shape})
}

// RemoveDebugShape removes a debug shape from the player by its unique identifier.
func (s *Session) RemoveDebugShape(shape debug.Shape) {
	if s == Nop {
		return
	}
	s.queueDebugShapeUpdate(debugShapeUpdate{id: shape.ShapeID()})
}

// VisibleDebugShapes returns a slice of all debug shapes that are currently being shown to the player.
func (s *Session) VisibleDebugShapes() []debug.Shape {
	s.debugShapesMu.RLock()
	if len(s.debugShapes) == 0 {
		// slices.Collect appends to a nil slice, so an empty map produced nil. Returning early keeps that
		// and skips the iterator entirely.
		s.debugShapesMu.RUnlock()
		return nil
	}
	// The size is known, so the slice is allocated once instead of grown. The same map is walked in the
	// same way, so the shapes come out in the same order. The lock is released before returning rather
	// than by a defer frame; nothing in the walk can panic.
	shapes := slices.AppendSeq(make([]debug.Shape, 0, len(s.debugShapes)), maps.Values(s.debugShapes))
	s.debugShapesMu.RUnlock()
	return shapes
}

// RemoveAllDebugShapes removes all rendered debug shapes from the player, as well as any shapes that have
// not yet been rendered.
func (s *Session) RemoveAllDebugShapes() {
	if s == Nop {
		return
	}
	// The critical section is a slice reset and a map walk that appends plain values, none of which can
	// panic, and there is no early return: the unlock does not need a defer frame.
	s.debugShapesMu.Lock()
	s.debugShapeUpdates = s.debugShapeUpdates[:0]
	for id := range s.debugShapes {
		s.debugShapeUpdates = append(s.debugShapeUpdates, debugShapeUpdate{id: id})
	}
	s.debugShapesMu.Unlock()
}

// SendDebugShapes sends any pending additions/removals of debug shapes to the player. Shapes should be sent
// every tick to allow for batching and time-efficient updates.
func (s *Session) SendDebugShapes(dim world.Dimension) {
	s.debugShapesMu.Lock()
	updates := s.debugShapeUpdates
	if len(updates) == 0 {
		s.debugShapesMu.Unlock()
		return
	}

	shapes := make([]protocol.PrimitiveShape, 0, len(updates))
	// The dimension is resolved once: a removal for an unregistered
	// dimension is refused rather than addressed to the overworld.
	dimID, dimOK := s.dimensionID(dim)
	for _, update := range updates {
		if update.shape == nil {
			delete(s.debugShapes, update.id)
			if !dimOK {
				continue
			}
			shapes = append(shapes, protocol.PrimitiveShape{
				NetworkID:      uint64(update.id),
				DimensionID:    protocol.Option(dimID),
				ExtraShapeData: &protocol.LastShape{},
			})
			continue
		}
		s.debugShapes[update.id] = update.shape
		shapes = append(shapes, debugShapeToProtocol(update.shape, dim, s.shapeAttachedEntityRuntimeID(update.shape)))
	}
	s.debugShapeUpdates = s.debugShapeUpdates[:0]
	s.debugShapesMu.Unlock()

	s.writePacket(&packet.PrimitiveShapes{Shapes: shapes})
}

// LockInput applies an input lock to the player, disabling the specified input. If the lock is already
// applied, this is a no-op.
func (s *Session) LockInput(l input.Lock) {
	// input.Lock is a concrete struct over a uint32, so the critical section is a single arithmetic update
	// that cannot panic and has no early return: the unlock does not need a defer frame.
	s.inputLocksMu.Lock()
	s.inputLocks |= l.Uint32()
	s.inputLocksMu.Unlock()
}

// UnlockInput removes an input lock from the player, re-enabling the specified input. If the lock is not
// currently applied, this is a no-op.
func (s *Session) UnlockInput(l input.Lock) {
	// As in LockInput, the critical section is one arithmetic update that cannot panic.
	s.inputLocksMu.Lock()
	s.inputLocks &^= l.Uint32()
	s.inputLocksMu.Unlock()
}

// ClearInputLocks removes all input locks from the player, re-enabling all inputs.
func (s *Session) ClearInputLocks() {
	// The critical section is a single store that cannot panic.
	s.inputLocksMu.Lock()
	s.inputLocks = 0
	s.inputLocksMu.Unlock()
}

// InputLocked checks if a specific input lock is currently applied to the player.
func (s *Session) InputLocked(l input.Lock) bool {
	// The critical section is a single read of a uint32 that cannot panic.
	s.inputLocksMu.RLock()
	locked := s.inputLocks&l.Uint32() != 0
	s.inputLocksMu.RUnlock()
	return locked
}

// SendInputLocks sends the current input lock state to the client.
func (s *Session) SendInputLocks() {
	// The lock only guards the read of the lock bits, so it is released before the packet is queued instead
	// of being held across a channel send. The packet still carries the value read under the lock, and the
	// unlock no longer needs a defer frame.
	s.inputLocksMu.RLock()
	locks := s.inputLocks
	s.inputLocksMu.RUnlock()
	s.writePacket(&packet.UpdateClientInputLocks{
		Locks: locks,
	})
}

// queueDebugShapeUpdate queues a debug shape mutation to be applied the next time debug shapes are sent.
func (s *Session) queueDebugShapeUpdate(update debugShapeUpdate) {
	// The critical section is a single append of a plain value, which cannot panic.
	s.debugShapesMu.Lock()
	s.debugShapeUpdates = append(s.debugShapeUpdates, update)
	s.debugShapesMu.Unlock()
}

// valueOrDefault returns the value passed if it is not the zero value of the type T, otherwise it returns
// the default value provided.
func valueOrDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// stackFromItem converts an item.Stack to its network ItemStack representation.
func stackFromItem(br world.BlockRegistry, it item.Stack) protocol.ItemStack {
	if it.Empty() {
		return protocol.ItemStack{}
	}

	t := it.Item()

	var blockRuntimeID uint32
	if b, ok := t.(world.Block); ok {
		blockRuntimeID = br.BlockRuntimeID(b)
	}

	rid, meta, _ := world.ItemRuntimeID(t)

	return protocol.ItemStack{
		ItemType: protocol.ItemType{
			NetworkID:     rid,
			MetadataValue: uint32(meta),
		},
		Count:          uint16(it.Count()),
		BlockRuntimeID: int32(blockRuntimeID),
		NBTData:        item.WriteNBT(it, false),
	}
}

// decodeItemNBT returns the item type passed with the NBT data of a network
// item stack decoded into it. The item type is returned unchanged if it does
// not read NBT, if the data is empty, or if decoding it fails.
//
// Decoding is allowed to fail in two ways. An item whose DecodeNBT hands back
// something that is not a world.Item is a defect in that item, not in the data,
// so the resolved item type is kept without its own tags and the count and the
// rest of the stack survive. And DecodeNBT is allowed to panic: hundreds of
// item and block types implement it, third-party items this package cannot see
// implement it too, and many of them index the map they are given without
// checking what is in it or reject a name they cannot resolve by panicking
// (block.DecoratedPot does). The map here is the one the client put on the
// wire, so without this recover a single crafted packet ends the goroutine
// reading that client's packets. Fixing every implementation is not an option
// while third parties write them, which is what makes this the boundary to
// hold.
//
// The panic value is dropped rather than logged. Every call is driven by a
// client packet, one of them on the PlayerAuthInput path that runs each tick,
// so logging would hand any client a way to flood the log. A defect in a
// first-party DecodeNBT still surfaces on the world save path, which does not
// recover, and item.ReadNBT already drops tags it cannot decode on this same
// path without reporting them.
func decodeItemNBT(t world.Item, data map[string]any) (decoded world.Item) {
	nbter, ok := t.(world.NBTer)
	if !ok || len(data) == 0 {
		return t
	}
	decoded = t
	defer func() {
		if recover() != nil {
			decoded = t
		}
	}()
	if d, ok := nbter.DecodeNBT(data).(world.Item); ok {
		decoded = d
	}
	return decoded
}

// stackToItem converts a network ItemStack representation back to an item.Stack.
func stackToItem(br world.BlockRegistry, it protocol.ItemStack) item.Stack {
	t, ok := world.ItemByRuntimeID(it.NetworkID, int16(it.MetadataValue))
	if !ok {
		t = block.Air{}
	}
	if it.BlockRuntimeID > 0 {
		// It shouldn't matter if it (for whatever reason) wasn't able to get the block runtime ID,
		// since on the next line, we assert that the block is an item. If it didn't succeed, it'll
		// return air anyway.
		b, _ := br.BlockByRuntimeID(uint32(it.BlockRuntimeID))
		if t, ok = b.(world.Item); !ok {
			t = block.Air{}
		}
	}
	t = decodeItemNBT(t, it.NBTData)
	s := item.NewStack(t, int(it.Count))
	// The network path applies the tags to a stack that is already resolved, so
	// there is no item name for ReadNBT to fail on.
	s, _ = item.ReadNBT(it.NBTData, &s)
	return s
}

// instanceFromItem converts an item.Stack to its network ItemInstance representation.
func instanceFromItem(br world.BlockRegistry, it item.Stack) protocol.ItemInstance {
	return protocol.ItemInstance{
		StackNetworkID: item_id(it),
		Stack:          stackFromItem(br, it),
	}
}

// stacksToRecipeStacks converts a list of item.Stacks to their protocol representation with damage stripped for recipes.
func stacksToRecipeStacks(br world.BlockRegistry, inputs []item.Stack) []protocol.ItemStack {
	items := make([]protocol.ItemStack, 0, len(inputs))
	for _, i := range inputs {
		items = append(items, deleteDamage(stackFromItem(br, i)))
	}
	return items
}

// stacksToIngredientItems converts a list of item.Stacks to recipe ingredient items used over the network.
func stacksToIngredientItems(_ world.BlockRegistry, inputs []recipe.Item) []protocol.ItemDescriptorCount {
	items := make([]protocol.ItemDescriptorCount, 0, len(inputs))
	for _, i := range inputs {
		var d protocol.ItemDescriptor = &protocol.InvalidItemDescriptor{}
		switch i := i.(type) {
		case item.Stack:
			if i.Empty() {
				items = append(items, protocol.ItemDescriptorCount{Descriptor: &protocol.InvalidItemDescriptor{}})
				continue
			}
			name, meta := i.Item().EncodeItem()
			if _, ok := i.Value("variants"); ok {
				meta = math.MaxInt16 // Used to indicate that the item has multiple selectable variants.
			}
			d = &protocol.DefaultItemDescriptor{
				Name:          name,
				MetadataValue: int32(meta),
			}
		case recipe.ItemTag:
			d = &protocol.ItemTagItemDescriptor{Tag: i.Tag()}
		}
		items = append(items, protocol.ItemDescriptorCount{
			Descriptor: d,
			Count:      int32(i.Count()),
		})
	}
	return items
}

// creativeContent returns all creative groups, and creative inventory items as protocol item stacks.
func creativeContent(br world.BlockRegistry) ([]protocol.CreativeGroup, []protocol.CreativeItem) {
	creativeGroups := creative.Groups()
	groups := make([]protocol.CreativeGroup, 0, len(creativeGroups))
	for _, group := range creativeGroups {
		groups = append(groups, protocol.CreativeGroup{
			Category: group.Category.Uint8(),
			Name:     group.Name,
			Icon:     deleteDamage(stackFromItem(br, group.Icon)),
		})
	}

	creativeItems := creative.Items()
	it := make([]protocol.CreativeItem, 0, len(creativeItems))
	for index, i := range creativeItems {
		// The search runs inline instead of through slices.IndexFunc. The closure captured the whole item
		// and was built again on every one of the thousands of iterations; the loop below visits the same
		// groups in the same order and stops at the same first match, so the index is unchanged.
		group := -1
		for gi := range creativeGroups {
			if creativeGroups[gi].Name == i.Group {
				group = gi
				break
			}
		}
		if group < 0 {
			continue
		}
		it = append(it, protocol.CreativeItem{
			CreativeItemNetworkID: uint32(index) + 1,
			Item:                  deleteDamage(stackFromItem(br, i.Stack)),
			GroupIndex:            uint32(group),
		})
	}
	return groups, it
}

// deleteDamage strips the damage from a protocol item.
func deleteDamage(st protocol.ItemStack) protocol.ItemStack {
	delete(st.NBTData, "Damage")
	return st
}

// protocolToSkin converts protocol.Skin to skin.Skin.
func protocolToSkin(sk protocol.Skin) (s skin.Skin, err error) {
	if sk.SkinID == "" {
		return skin.Skin{}, fmt.Errorf("SkinID must not be an empty string")
	}

	// protocol.Skin.validate compares width*height*4 against len(data) in uint32 arithmetic, while the
	// constructors below allocate width*height*4 in int arithmetic. Dimensions whose product is a multiple
	// of 2^32, such as 1073741824x1, wrap to zero for gophertunnel and validate against empty pixel data,
	// then ask for the full unwrapped size here. Repeating the check in the same int arithmetic the
	// constructors use ties every allocation below to the length of the data actually received.
	skinWidth, skinHeight := int(sk.SkinImageWidth), int(sk.SkinImageHeight)
	// The required size is computed once and reused by the message, so the check and the message still
	// report the same number in the same int arithmetic.
	skinSize := skinWidth * skinHeight * 4
	if skinSize != len(sk.SkinData) {
		return skin.Skin{}, fmt.Errorf("skin of %vx%v needs %v bytes of pixel data, but got %v", skinWidth, skinHeight, skinSize, len(sk.SkinData))
	}

	s = skin.New(skinWidth, skinHeight)
	s.Persona = sk.PersonaSkin
	s.Pix = sk.SkinData
	s.Model = sk.SkinGeometry
	s.PlayFabID = sk.PlayFabID
	s.FullID = sk.FullID

	capeWidth, capeHeight := int(sk.CapeImageWidth), int(sk.CapeImageHeight)
	capeSize := capeWidth * capeHeight * 4
	if capeSize != len(sk.CapeData) {
		return skin.Skin{}, fmt.Errorf("cape of %vx%v needs %v bytes of pixel data, but got %v", capeWidth, capeHeight, capeSize, len(sk.CapeData))
	}

	s.Cape = skin.NewCape(capeWidth, capeHeight)
	s.Cape.Pix = sk.CapeData

	// The map is only used to check that the geometry is a JSON object. Unmarshal allocates it itself when
	// the data holds one, so pre-allocating it only wasted a map on data that fails the check.
	var m map[string]any
	if err = json.Unmarshal(sk.SkinGeometry, &m); err != nil {
		return skin.Skin{}, fmt.Errorf("SkinGeometry was not a valid JSON string: %v", err)
	}

	if s.ModelConfig, err = skin.DecodeModelConfig(sk.SkinResourcePatch); err != nil {
		return skin.Skin{}, fmt.Errorf("SkinResourcePatch was not a valid JSON string: %v", err)
	}

	if len(sk.Animations) > 0 {
		// The upper bound is known, so the slice is allocated once instead of grown. A skin without
		// animations still leaves the field nil, exactly as the plain append did.
		s.Animations = make([]skin.Animation, 0, len(sk.Animations))
	}
	for _, anim := range sk.Animations {
		var t skin.AnimationType
		switch anim.AnimationType {
		case protocol.SkinAnimationHead:
			t = skin.AnimationHead
		case protocol.SkinAnimationBody32x32:
			t = skin.AnimationBody32x32
		case protocol.SkinAnimationBody128x128:
			t = skin.AnimationBody128x128
		default:
			return skin.Skin{}, fmt.Errorf("invalid animation type: %v", anim.AnimationType)
		}

		animWidth, animHeight := int(anim.ImageWidth), int(anim.ImageHeight)
		animSize := animWidth * animHeight * 4
		if animSize != len(anim.ImageData) {
			return skin.Skin{}, fmt.Errorf("animation of %vx%v needs %v bytes of pixel data, but got %v", animWidth, animHeight, animSize, len(anim.ImageData))
		}

		animation := skin.NewAnimation(animWidth, animHeight, int(anim.ExpressionType), t)
		animation.FrameCount = int(anim.FrameCount)
		animation.Pix = anim.ImageData

		s.Animations = append(s.Animations, animation)
	}
	return
}

// shapeAttachedEntityRuntimeID returns the runtime ID of the entity attached to a debug shape.
func (s *Session) shapeAttachedEntityRuntimeID(shape debug.Shape) uint64 {
	var handle *world.EntityHandle
	switch shape := shape.(type) {
	case *debug.Arrow:
		handle = shape.Entity
	case *debug.Box:
		handle = shape.Entity
	case *debug.Circle:
		handle = shape.Entity
	case *debug.Line:
		handle = shape.Entity
	case *debug.Sphere:
		handle = shape.Entity
	case *debug.Text:
		handle = shape.Entity
	case *debug.Cylinder:
		handle = shape.Entity
	case *debug.Pyramid:
		handle = shape.Entity
	case *debug.Ellipsoid:
		handle = shape.Entity
	case *debug.Cone:
		handle = shape.Entity
	}
	if handle == nil {
		return 0
	}
	return s.handleRuntimeID(handle)
}

// debugShapeToProtocol converts a debug shape to its protocol representation. It also provides defaults
// for some fields such as colour, scale and other per-shape properties.
func debugShapeToProtocol(shape debug.Shape, dim world.Dimension, attachedEntityID uint64) protocol.PrimitiveShape {
	dimID, _ := world.DimensionID(dim)
	ps := protocol.PrimitiveShape{
		NetworkID:   uint64(shape.ShapeID()),
		DimensionID: protocol.Option(int32(dimID)),
	}
	if attachedEntityID > 0 {
		ps.AttachedToEntityID = protocol.Option(int64(attachedEntityID))
	}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	switch shape := shape.(type) {
	case *debug.Arrow:
		ps.Type = protocol.Option(protocol.PrimitiveShapeArrow)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.ExtraShapeData = &protocol.ArrowShape{
			ArrowEndLocation: protocol.Option(vec64To32(shape.EndPosition)),
			ArrowHeadLength:  protocol.Option(valueOrDefault(float32(shape.HeadLength), 1)),
			ArrowHeadRadius:  protocol.Option(valueOrDefault(float32(shape.HeadRadius), 0.5)),
			Segments:         protocol.Option(valueOrDefault(uint8(shape.HeadSegments), 4)),
		}
	case *debug.Box:
		ps.Type = protocol.Option(protocol.PrimitiveShapeBox)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &protocol.BoxShape{BoxBound: valueOrDefault(vec64To32(shape.Bounds), mgl32.Vec3{1, 1, 1})}
	case *debug.Circle:
		ps.Type = protocol.Option(protocol.PrimitiveShapeCircle)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &protocol.SphereShape{Segments: valueOrDefault(uint8(shape.Segments), 20)}
	case *debug.Line:
		ps.Type = protocol.Option(protocol.PrimitiveShapeLine)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.ExtraShapeData = &protocol.LineShape{LineEndLocation: vec64To32(shape.EndPosition)}
	case *debug.Sphere:
		ps.Type = protocol.Option(protocol.PrimitiveShapeSphere)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &protocol.SphereShape{Segments: valueOrDefault(uint8(shape.Segments), 20)}
	case *debug.Text:
		ps.Type = protocol.Option(protocol.PrimitiveShapeText)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		if shape.LockRotation {
			ps.Rotation = protocol.Option(vec64To32(shape.Rotation))
		}
		textData := &protocol.TextShape{
			Text:             shape.Text,
			UseRotation:      shape.LockRotation,
			DepthTest:        !shape.DisableDepthTest,
			ShowBackface:     !shape.HideBackface,
			ShowBackfaceText: !shape.HideBackfaceText,
		}
		switch {
		case shape.HideBackground:
			textData.BackgroundColour = protocol.Option(color.RGBA{})
		case shape.BackgroundColour != (color.RGBA{}):
			textData.BackgroundColour = protocol.Option(shape.BackgroundColour)
		}
		ps.ExtraShapeData = textData
	case *debug.Cylinder:
		ps.Type = protocol.Option(protocol.PrimitiveShapeCylinder)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		base := valueOrDefault(shape.BaseRadius, mgl64.Vec2{1, 1})
		top := valueOrDefault(shape.TopRadius, base)
		ps.ExtraShapeData = &protocol.CylinderShape{
			RadiusX:     mgl32.Vec2{float32(base[0]), float32(top[0])},
			RadiusZ:     mgl32.Vec2{float32(base[1]), float32(top[1])},
			Height:      valueOrDefault(float32(shape.Height), 1),
			NumSegments: valueOrDefault(uint8(shape.Segments), 20),
		}
	case *debug.Pyramid:
		ps.Type = protocol.Option(protocol.PrimitiveShapePyramid)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		pyramid := &protocol.PyramidShape{
			Width:  valueOrDefault(float32(shape.Width), 1),
			Height: valueOrDefault(float32(shape.Height), 1),
		}
		if shape.Depth != 0 {
			pyramid.Depth = protocol.Option(float32(shape.Depth))
		}
		ps.ExtraShapeData = pyramid
	case *debug.Ellipsoid:
		ps.Type = protocol.Option(protocol.PrimitiveShapeEllipsoid)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &protocol.EllipsoidShape{
			Radii:           valueOrDefault(vec64To32(shape.Radii), mgl32.Vec3{1, 1, 1}),
			SegmentsPerAxis: valueOrDefault(uint8(shape.SegmentsPerAxis), 20),
		}
	case *debug.Cone:
		ps.Type = protocol.Option(protocol.PrimitiveShapeCone)
		ps.Colour = protocol.Option(valueOrDefault(shape.Colour, white))
		ps.Location = protocol.Option(vec64To32(shape.Position))
		ps.Scale = protocol.Option(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &protocol.ConeShape{
			Radii:       valueOrDefault(vec2To32(shape.Radii), mgl32.Vec2{1, 1}),
			Height:      valueOrDefault(float32(shape.Height), 1),
			NumSegments: valueOrDefault(uint8(shape.Segments), 20),
		}
	default:
		panic(fmt.Sprintf("unknown debug shape type %T", shape))
	}
	return ps
}

// gameTypeFromMode returns the game type ID from the game mode passed.
func gameTypeFromMode(mode world.GameMode) int32 {
	if mode.AllowsFlying() && mode.CreativeInventory() {
		return packet.GameTypeCreative
	}
	return packet.GameTypeSurvival
}

// The following functions use the go:linkname directive in order to make sure the item.byID and item.toID
// functions do not need to be exported.

// noinspection ALL
//
//go:linkname item_id github.com/jv2w/Impulse/server/item.id
func item_id(s item.Stack) int32
