package playerdb

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/item"
	"github.com/jv2w/Impulse/server/item/inventory"
	"github.com/jv2w/Impulse/server/player"
	"github.com/jv2w/Impulse/server/world"
	"time"
)

func (p *Provider) fromJson(d jsonData, id uuid.UUID) player.Config {
	mode, _ := world.GameModeByID(int(d.GameMode))
	// Which input was the problem: d.UUID is a string from the player's save
	// file, so one corrupt byte made uuid.MustParse panic and bricked login
	// for that player. The LevelDB key is the authoritative identity, so a
	// UUID that no longer parses falls back to it and the next save rewrites
	// the embedded copy. A UUID that parses keeps the existing behavior.
	uid, err := uuid.Parse(d.UUID)
	if err != nil {
		uid = id
	}
	conf := player.Config{
		UUID:                uid,
		XUID:                d.XUID,
		Name:                d.Username,
		Position:            d.Position,
		Rotation:            cube.Rotation{d.Yaw, d.Pitch},
		Velocity:            d.Velocity,
		Health:              d.Health,
		MaxHealth:           d.MaxHealth,
		Food:                d.Hunger,
		FoodTick:            d.FoodTick,
		Exhaustion:          d.ExhaustionLevel,
		Saturation:          d.SaturationLevel,
		Experience:          d.Experience,
		AirSupply:           d.AirSupply,
		MaxAirSupply:        d.MaxAirSupply,
		EnchantmentSeed:     d.EnchantmentSeed,
		GameMode:            mode,
		Effects:             dataToEffects(d.Effects),
		FireTicks:           d.FireTicks,
		FallDistance:        d.FallDistance,
		Inventory:           inventory.New(36, nil),
		EnderChestInventory: inventory.New(27, nil),
		OffHand:             inventory.New(1, nil),
		Armour:              inventory.NewArmour(nil),
	}
	echest := make([]item.Stack, 27)
	decodeItems(d.EnderChestInventory, echest)
	invData := dataToInv(d.Inventory)

	for slot, stack := range invData.Items {
		// Discarding the error is safe: slot ranges over a slice built for
		// this 36-slot inventory, so it is always in range, the only error
		// SetItem returns.
		_ = conf.Inventory.SetItem(slot, stack)
	}
	// Discarding the error is safe: slot 0 always exists in the 1-slot
	// off-hand inventory.
	_ = conf.OffHand.SetItem(0, invData.OffHand)
	conf.Armour.Set(invData.Helmet, invData.Chestplate, invData.Leggings, invData.Boots)
	// Which input was the problem: MainHandSlot is a uint32 from the save file
	// with no range check, so a corrupt value landed outside the 0-8 hotbar
	// that SetHeldSlot enforces and was echoed back to the client. Fall back
	// to the default slot; valid values pass through unchanged.
	conf.HeldSlot = int(invData.MainHandSlot)
	if conf.HeldSlot < 0 || conf.HeldSlot > 8 {
		conf.HeldSlot = 0
	}

	for slot, stack := range echest {
		// Discarding the error is safe: slot ranges over a slice built for
		// this 27-slot inventory, so it is always in range, the only error
		// SetItem returns.
		_ = conf.EnderChestInventory.SetItem(slot, stack)
	}
	return conf
}

func (p *Provider) toJson(d player.Config) jsonData {
	mode, _ := world.GameModeID(d.GameMode)
	offHand, _ := d.OffHand.Item(0)
	return jsonData{
		UUID:     d.UUID.String(),
		Username: d.Name,
		// Which input was the problem: every save. The XUID was read back on
		// load but never written, so it always loaded empty.
		XUID:            d.XUID,
		Position:        d.Position,
		Velocity:        d.Velocity,
		Yaw:             d.Rotation.Yaw(),
		Pitch:           d.Rotation.Pitch(),
		Health:          d.Health,
		MaxHealth:       d.MaxHealth,
		Hunger:          d.Food,
		FoodTick:        d.FoodTick,
		ExhaustionLevel: d.Exhaustion,
		SaturationLevel: d.Saturation,
		Experience:      d.Experience,
		AirSupply:       d.AirSupply,
		MaxAirSupply:    d.MaxAirSupply,
		EnchantmentSeed: d.EnchantmentSeed,
		GameMode:        uint8(mode),
		Effects:         effectsToData(d.Effects),
		FireTicks:       d.FireTicks,
		FallDistance:    d.FallDistance,
		Inventory: invToData(InventoryData{
			Items:        d.Inventory.Slots(),
			Boots:        d.Armour.Boots(),
			Leggings:     d.Armour.Leggings(),
			Chestplate:   d.Armour.Chestplate(),
			Helmet:       d.Armour.Helmet(),
			OffHand:      offHand,
			MainHandSlot: uint32(d.HeldSlot),
		}),
		EnderChestInventory: encodeItems(d.EnderChestInventory.Slots()),
	}
}

type jsonData struct {
	UUID                             string
	XUID                             string
	Username                         string
	Position, Velocity               mgl64.Vec3
	Yaw, Pitch                       float64
	Health, MaxHealth                float64
	Hunger                           int
	FoodTick                         int
	ExhaustionLevel, SaturationLevel float64
	EnchantmentSeed                  int64
	Experience                       int
	AirSupply, MaxAirSupply          int
	GameMode                         uint8
	Inventory                        jsonInventoryData
	EnderChestInventory              []jsonSlot
	Effects                          []jsonEffect
	FireTicks                        int64
	FallDistance                     float64
}

type jsonInventoryData struct {
	Items        []jsonSlot
	Boots        []byte
	Leggings     []byte
	Chestplate   []byte
	Helmet       []byte
	OffHand      []byte
	MainHandSlot uint32
}

type jsonSlot struct {
	Item []byte
	Slot int
}

type jsonEffect struct {
	ID              int
	Level           int
	Duration        time.Duration
	Ambient         bool
	ParticlesHidden bool
	Infinite        bool
}
