package entity

import (
	"fmt"
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/jv2w/Impulse/server/block"
	"github.com/jv2w/Impulse/server/block/cube"
	"github.com/jv2w/Impulse/server/world"
)

// physEpsilon is the absolute tolerance used to compare golden physics values.
//
// The goldens were produced by an independent reimplementation of the documented
// force order and printed with %.17g, so they round-trip to the exact float64 the
// production code should reach; the slack only absorbs harmless reassociation of
// the same operations. Every defect this file guards against is orders of
// magnitude larger: a 1% change to the item gravity constant moves the first tick
// by 4e-4, and swapping drag and gravity moves it by 8e-4. An == comparison would
// be brittle against reassociation; anything near 1e-4 would start hiding those
// defects.
const physEpsilon = 1e-9

// movementStep is one tick of a golden position/velocity sequence.
type movementStep struct {
	pos, vel mgl64.Vec3
}

func TestApplyVerticalForcesOrder(t *testing.T) {
	// applyVerticalForces is the whole reason MovementComputer.DragBeforeGravity
	// exists. Pin both orders with exact arithmetic so a swap cannot pass.
	const gravity, drag = 0.04, 0.02

	tests := []struct {
		name              string
		dragBeforeGravity bool
		vel               float64
		want              float64
	}{
		// vel*(1-drag) - gravity
		{name: "drag before gravity, rising", dragBeforeGravity: true, vel: 1, want: 1*0.98 - gravity},
		{name: "drag before gravity, falling", dragBeforeGravity: true, vel: -1, want: -1*0.98 - gravity},
		{name: "drag before gravity, at rest", dragBeforeGravity: true, vel: 0, want: -gravity},
		// (vel - gravity)*(1-drag)
		{name: "gravity before drag, rising", dragBeforeGravity: false, vel: 1, want: (1 - gravity) * 0.98},
		{name: "gravity before drag, falling", dragBeforeGravity: false, vel: -1, want: (-1 - gravity) * 0.98},
		{name: "gravity before drag, at rest", dragBeforeGravity: false, vel: 0, want: -gravity * 0.98},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &MovementComputer{Gravity: gravity, Drag: drag, DragBeforeGravity: test.dragBeforeGravity}
			got := c.applyVerticalForces(mgl64.Vec3{7, test.vel, -7})
			if math.Abs(got[1]-test.want) > physEpsilon {
				t.Fatalf("applyVerticalForces() Y = %.17g, want %.17g", got[1], test.want)
			}
			// applyVerticalForces must leave X and Z untouched: horizontal drag
			// is applied separately, and applying it here too would square it.
			if got[0] != 7 || got[2] != -7 {
				t.Fatalf("applyVerticalForces() horizontal = (%v, %v), want (7, -7)", got[0], got[2])
			}
		})
	}
}

func TestApplyVerticalForcesOrdersDiverge(t *testing.T) {
	// A guard for the guard: if the two orders ever produced the same number the
	// table above would pass no matter which branch ran.
	const gravity, drag = 0.04, 0.02
	before := (&MovementComputer{Gravity: gravity, Drag: drag, DragBeforeGravity: true}).applyVerticalForces(mgl64.Vec3{})
	after := (&MovementComputer{Gravity: gravity, Drag: drag}).applyVerticalForces(mgl64.Vec3{})
	if diff := math.Abs(before[1] - after[1]); diff <= physEpsilon {
		t.Fatalf("drag-before-gravity and gravity-before-drag differ by %.17g, want a difference the golden tests can detect", diff)
	}
	if want := -gravity; math.Abs(before[1]-want) > physEpsilon {
		t.Fatalf("drag before gravity from rest = %.17g, want %.17g", before[1], want)
	}
	if want := -gravity * (1 - drag); math.Abs(after[1]-want) > physEpsilon {
		t.Fatalf("gravity before drag from rest = %.17g, want %.17g", after[1], want)
	}
}

func TestTickMovementFreeFallSequence(t *testing.T) {
	// Ten-tick golden sequences in open air. Every production entity uses
	// DragBeforeGravity, so the constants are the only thing that varies.
	tests := []struct {
		name          string
		gravity, drag float64
		vel           mgl64.Vec3
		want          []movementStep
	}{
		{
			name: "item at rest", gravity: 0.04, drag: 0.02,
			want: []movementStep{
				{pos: mgl64.Vec3{0.5, 99.959999999999994, 0.5}, vel: mgl64.Vec3{0, -0.040000000000000001, 0}},
				{pos: mgl64.Vec3{0.5, 99.880799999999994, 0.5}, vel: mgl64.Vec3{0, -0.079199999999999993, 0}},
				{pos: mgl64.Vec3{0.5, 99.763183999999995, 0.5}, vel: mgl64.Vec3{0, -0.117616, 0}},
				{pos: mgl64.Vec3{0.5, 99.607920319999991, 0.5}, vel: mgl64.Vec3{0, -0.15526367999999999, 0}},
				{pos: mgl64.Vec3{0.5, 99.415761913599994, 0.5}, vel: mgl64.Vec3{0, -0.19215840639999998, 0}},
				{pos: mgl64.Vec3{0.5, 99.18744667532799, 0.5}, vel: mgl64.Vec3{0, -0.22831523827199998, 0}},
				{pos: mgl64.Vec3{0.5, 98.923697741821428, 0.5}, vel: mgl64.Vec3{0, -0.26374893350655998, 0}},
				{pos: mgl64.Vec3{0.5, 98.625223786985003, 0.5}, vel: mgl64.Vec3{0, -0.29847395483642875, 0}},
				{pos: mgl64.Vec3{0.5, 98.292719311245307, 0.5}, vel: mgl64.Vec3{0, -0.33250447573970016, 0}},
				{pos: mgl64.Vec3{0.5, 97.926864925020396, 0.5}, vel: mgl64.Vec3{0, -0.36585438622490613, 0}},
			},
		},
		{
			name: "item launched", gravity: 0.04, drag: 0.02, vel: mgl64.Vec3{0.4, 0.6, -0.25},
			want: []movementStep{
				{pos: mgl64.Vec3{0.89200000000000002, 100.548, 0.255}, vel: mgl64.Vec3{0.39200000000000002, 0.54799999999999993, -0.245}},
				{pos: mgl64.Vec3{1.27616, 101.04504, 0.014900000000000024}, vel: mgl64.Vec3{0.38416, 0.49703999999999998, -0.24009999999999998}},
				{pos: mgl64.Vec3{1.6526368, 101.4921392, -0.22039799999999996}, vel: mgl64.Vec3{0.3764768, 0.44709919999999997, -0.23529799999999998}},
				{pos: mgl64.Vec3{2.0215840639999998, 101.890296416, -0.45099003999999993}, vel: mgl64.Vec3{0.368947264, 0.39815721599999998, -0.23059203999999997}},
				{pos: mgl64.Vec3{2.3831523827199996, 102.24049048767999, -0.67697023919999988}, vel: mgl64.Vec3{0.36156831872, 0.35019407168, -0.22598019919999995}},
				{pos: mgl64.Vec3{2.7374893350655998, 102.54368067792639, -0.8984308344159998}, vel: mgl64.Vec3{0.35433695234559998, 0.30319019024640004, -0.22146059521599995}},
				{pos: mgl64.Vec3{3.0847395483642877, 102.80080706436786, -1.1154622177276798}, vel: mgl64.Vec3{0.34725021329868799, 0.25712638644147207, -0.21703138331167995}},
				{pos: mgl64.Vec3{3.4250447573970018, 103.0127909230805, -1.3281529733731261}, vel: mgl64.Vec3{0.34030520903271422, 0.21198385871264261, -0.21269075564544634}},
				{pos: mgl64.Vec3{3.7585438622490619, 103.1805351046189, -1.5365899139056636}, vel: mgl64.Vec3{0.33349910485205991, 0.16774418153838974, -0.20843694053253742}},
				{pos: mgl64.Vec3{4.0853729850040805, 103.30492440252652, -1.7408581156275502}, vel: mgl64.Vec3{0.32682912275501869, 0.12438929790762193, -0.20426820172188667}},
			},
		},
		{
			name: "arrow", gravity: 0.05, drag: 0.01, vel: mgl64.Vec3{1, 0.5, 0},
			want: []movementStep{
				{pos: mgl64.Vec3{1.49, 100.44499999999999, 0.5}, vel: mgl64.Vec3{0.98999999999999999, 0.44500000000000001, 0}},
				{pos: mgl64.Vec3{2.4701, 100.83555, 0.5}, vel: mgl64.Vec3{0.98009999999999997, 0.39055000000000001, 0}},
				{pos: mgl64.Vec3{3.4403989999999998, 101.1721945, 0.5}, vel: mgl64.Vec3{0.97029899999999991, 0.33664450000000001, 0}},
				{pos: mgl64.Vec3{4.4009950099999999, 101.455472555, 0.5}, vel: mgl64.Vec3{0.96059600999999994, 0.28327805500000003, 0}},
				{pos: mgl64.Vec3{5.3519850598999996, 101.68591782945001, 0.5}, vel: mgl64.Vec3{0.95099004989999991, 0.23044527445000001, 0}},
				{pos: mgl64.Vec3{6.2934652093009991, 101.8640586511555, 0.5}, vel: mgl64.Vec3{0.94148014940099989, 0.1781408217055, 0}},
				{pos: mgl64.Vec3{7.2255305572079891, 101.99041806464395, 0.5}, vel: mgl64.Vec3{0.93206534790698992, 0.12635941348844498, 0}},
				{pos: mgl64.Vec3{8.1482752516359085, 102.06551388399751, 0.5}, vel: mgl64.Vec3{0.92274469442791995, 0.075095819353560536, 0}},
				{pos: mgl64.Vec3{9.0617924991195498, 102.08985874515754, 0.5}, vel: mgl64.Vec3{0.91351724748364072, 0.024344861160024933, 0}},
				{pos: mgl64.Vec3{9.9661745741283543, 102.06396015770596, 0.5}, vel: mgl64.Vec3{0.9043820750088043, -0.025898587451575319, 0}},
			},
		},
		{
			name: "snowball", gravity: 0.03, drag: 0.01, vel: mgl64.Vec3{0, 0, 1.5},
			want: []movementStep{
				{pos: mgl64.Vec3{0.5, 99.969999999999999, 1.9849999999999999}, vel: mgl64.Vec3{0, -0.029999999999999999, 1.4849999999999999}},
				{pos: mgl64.Vec3{0.5, 99.910299999999992, 3.4551499999999997}, vel: mgl64.Vec3{0, -0.059699999999999996, 1.4701499999999998}},
				{pos: mgl64.Vec3{0.5, 99.821196999999998, 4.9105984999999999}, vel: mgl64.Vec3{0, -0.089102999999999988, 1.4554484999999999}},
				{pos: mgl64.Vec3{0.5, 99.702985029999994, 6.3514925149999995}, vel: mgl64.Vec3{0, -0.11821196999999999, 1.4408940149999998}},
				{pos: mgl64.Vec3{0.5, 99.555955179699993, 7.777977589849999}, vel: mgl64.Vec3{0, -0.14702985029999999, 1.4264850748499998}},
				{pos: mgl64.Vec3{0.5, 99.380395627902999, 9.1901978139514995}, vel: mgl64.Vec3{0, -0.17555955179699997, 1.4122202241014998}},
				{pos: mgl64.Vec3{0.5, 99.176591671623967, 10.588295835811984}, vel: mgl64.Vec3{0, -0.20380395627902997, 1.3980980218604848}},
				{pos: mgl64.Vec3{0.5, 98.944825754907725, 11.972412877453863}, vel: mgl64.Vec3{0, -0.23176591671623967, 1.38411704164188}},
				{pos: mgl64.Vec3{0.5, 98.685377497358644, 13.342688748679324}, vel: mgl64.Vec3{0, -0.2594482575490773, 1.3702758712254612}},
				{pos: mgl64.Vec3{0.5, 98.39852372238505, 14.699261861192531}, vel: mgl64.Vec3{0, -0.28685377497358655, 1.3565731125132066}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &MovementComputer{Gravity: test.gravity, Drag: test.drag, DragBeforeGravity: true}
			assertMovementSequence(t, c, mgl64.Vec3{0.5, 100, 0.5}, test.vel, test.want, nil)
		})
	}
}

func TestTickMovementDragAfterGravitySequence(t *testing.T) {
	// The same constants as "item at rest" above with the force order swapped.
	// Pinning the wrong sequence too proves the golden values discriminate the
	// order rather than merely the constants.
	want := []movementStep{
		{pos: mgl64.Vec3{0.5, 99.960800000000006, 0.5}, vel: mgl64.Vec3{0, -0.039199999999999999, 0}},
		{pos: mgl64.Vec3{0.5, 99.883184, 0.5}, vel: mgl64.Vec3{0, -0.077615999999999991, 0}},
		{pos: mgl64.Vec3{0.5, 99.767920320000002, 0.5}, vel: mgl64.Vec3{0, -0.11526367999999999, 0}},
		{pos: mgl64.Vec3{0.5, 99.615761913599997, 0.5}, vel: mgl64.Vec3{0, -0.15215840639999997, 0}},
		{pos: mgl64.Vec3{0.5, 99.427446675328, 0.5}, vel: mgl64.Vec3{0, -0.18831523827199997, 0}},
		{pos: mgl64.Vec3{0.5, 99.203697741821443, 0.5}, vel: mgl64.Vec3{0, -0.22374893350655997, 0}},
		{pos: mgl64.Vec3{0.5, 98.945223786985011, 0.5}, vel: mgl64.Vec3{0, -0.25847395483642877, 0}},
		{pos: mgl64.Vec3{0.5, 98.652719311245306, 0.5}, vel: mgl64.Vec3{0, -0.29250447573970018, 0}},
		{pos: mgl64.Vec3{0.5, 98.326864925020402, 0.5}, vel: mgl64.Vec3{0, -0.32585438622490615, 0}},
		{pos: mgl64.Vec3{0.5, 97.968327626519994, 0.5}, vel: mgl64.Vec3{0, -0.358537298500408, 0}},
	}
	c := &MovementComputer{Gravity: 0.04, Drag: 0.02}
	assertMovementSequence(t, c, mgl64.Vec3{0.5, 100, 0.5}, mgl64.Vec3{}, want, nil)
}

func TestApplyHorizontalForcesFrictionByBlock(t *testing.T) {
	// The friction of the block one below cube.PosFromVec3(pos) is multiplied
	// into (1-Drag), but only while the computer already considers the entity on
	// the ground.
	tests := []struct {
		name     string
		below    world.Block
		onGround bool
		want     float64
	}{
		{name: "airborne ignores the block below", below: block.BlueIce{}, want: 0.98},
		{name: "block without Friction defaults to 0.6", below: block.Stone{}, onGround: true, want: 0.98 * 0.6},
		// PINNED BUG: air is a block without a Friction method, so an entity
		// marked on the ground over open air is slowed as if it stood on stone
		// rather than sliding freely.
		{name: "air below also defaults to 0.6", below: block.Air{}, onGround: true, want: 0.98 * 0.6},
		{name: "blue ice", below: block.BlueIce{}, onGround: true, want: 0.98 * 0.989},
		{name: "packed ice", below: block.PackedIce{}, onGround: true, want: 0.98 * 0.98},
		{name: "slime", below: block.Slime{}, onGround: true, want: 0.98 * 0.8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := syncWorld(t)
			pos := mgl64.Vec3{0.5, 64, 0.5}
			mustDo(t, w, func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 63, 0}, test.below, nil)

				c := &MovementComputer{Gravity: 0.04, Drag: 0.02}
				c.onGround = test.onGround
				got := c.applyHorizontalForces(tx, pos, mgl64.Vec3{1, 5, -1})

				for axis, want := range map[int]float64{0: test.want, 2: -test.want} {
					if math.Abs(got[axis]-want) > physEpsilon {
						t.Fatalf("applyHorizontalForces() axis %d = %.17g, want %.17g", axis, got[axis], want)
					}
				}
				// Vertical velocity is the job of applyVerticalForces.
				if got[1] != 5 {
					t.Fatalf("applyHorizontalForces() Y = %v, want 5 unchanged", got[1])
				}
			})
		})
	}
}

func TestApplyHorizontalForcesReadsBlockBelowFeet(t *testing.T) {
	// The friction lookup is cube.PosFromVec3(pos).Side(cube.FaceDown), so it is
	// the block one below the entity's own block that counts, never the block the
	// entity's feet are inside.
	w := syncWorld(t)
	mustDo(t, w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Slime{}, nil)
		tx.SetBlock(cube.Pos{0, 63, 0}, block.BlueIce{}, nil)

		c := &MovementComputer{Drag: 0.02}
		c.onGround = true
		got := c.applyHorizontalForces(tx, mgl64.Vec3{0.5, 64, 0.5}, mgl64.Vec3{1, 0, 0})
		if want := 0.98 * 0.989; math.Abs(got[0]-want) > physEpsilon {
			t.Fatalf("applyHorizontalForces() X = %.17g, want %.17g (blue ice one below, not the slime at the feet)", got[0], want)
		}
	})
}

func TestTickMovementSlideSequence(t *testing.T) {
	// Sliding across a flat platform. The first tick still uses drag only: a
	// fresh MovementComputer has not resolved a collision yet, so onGround is
	// false and applyHorizontalForces runs before CheckCollision sets it. Only
	// from the second tick onward does the block friction apply. That one-tick
	// lag is behaviour, not an accident of the test.
	tests := []struct {
		name  string
		floor world.Block
		want  []movementStep
	}{
		{
			name: "stone, default friction 0.6", floor: block.Stone{},
			want: []movementStep{
				{pos: mgl64.Vec3{0.98999999999999999, 64, 0.745}, vel: mgl64.Vec3{0.48999999999999999, 0, 0.245}},
				{pos: mgl64.Vec3{1.2781199999999999, 64, 0.88905999999999996}, vel: mgl64.Vec3{0.28811999999999999, 0, 0.14405999999999999}},
				{pos: mgl64.Vec3{1.4475345599999998, 64, 0.9737672799999999}, vel: mgl64.Vec3{0.16941455999999999, 0, 0.084707279999999996}},
				{pos: mgl64.Vec3{1.5471503212799997, 64, 1.0235751606399999}, vel: mgl64.Vec3{0.099615761279999993, 0, 0.049807880639999996}},
				{pos: mgl64.Vec3{1.6057243889126398, 64, 1.0528621944563199}, vel: mgl64.Vec3{0.05857406763263999, 0, 0.029287033816319995}},
				{pos: mgl64.Vec3{1.6401659406806322, 64, 1.0700829703403161}, vel: mgl64.Vec3{0.034441551767992309, 0, 0.017220775883996155}},
				{pos: mgl64.Vec3{1.6604175731202115, 64, 1.0802087865601058}, vel: mgl64.Vec3{0.020251632439579476, 0, 0.010125816219789738}},
				{pos: mgl64.Vec3{1.6723255329946842, 64, 1.0861627664973421}, vel: mgl64.Vec3{0.011907959874472731, 0, 0.0059539799372363656}},
				{pos: mgl64.Vec3{1.6793274134008742, 64, 1.0896637067004371}, vel: mgl64.Vec3{0.0070018804061899656, 0, 0.0035009402030949828}},
				{pos: mgl64.Vec3{1.6834445190797138, 64, 1.091722259539857}, vel: mgl64.Vec3{0.0041171056788397, 0, 0.00205855283941985}},
			},
		},
		{
			name: "blue ice, friction 0.989", floor: block.BlueIce{},
			want: []movementStep{
				{pos: mgl64.Vec3{0.98999999999999999, 64, 0.745}, vel: mgl64.Vec3{0.48999999999999999, 0, 0.245}},
				{pos: mgl64.Vec3{1.4649178, 64, 0.98245890000000002}, vel: mgl64.Vec3{0.4749178, 0, 0.2374589}},
				{pos: mgl64.Vec3{1.925217630116, 64, 1.2126088150580001}, vel: mgl64.Vec3{0.46029983011600001, 0, 0.230149915058}},
				{pos: mgl64.Vec3{2.3713494314610295, 64, 1.435674715730515}, vel: mgl64.Vec3{0.4461318013450295, 0, 0.22306590067251475}},
				{pos: mgl64.Vec3{2.8037492959606589, 64, 1.6518746479803297}, vel: mgl64.Vec3{0.43239986449962947, 0, 0.21619993224981474}},
				{pos: mgl64.Vec3{3.22283989263099, 64, 1.861419946315495}, vel: mgl64.Vec3{0.41909059667033088, 0, 0.20954529833516544}},
				{pos: mgl64.Vec3{3.629030880735808, 64, 2.064515440367904}, vel: mgl64.Vec3{0.40619098810481807, 0, 0.20309549405240904}},
				{pos: mgl64.Vec3{4.0227193102267602, 64, 2.2613596551133801}, vel: mgl64.Vec3{0.39368842949095179, 0, 0.19684421474547589}},
				{pos: mgl64.Vec3{4.4042900098579807, 64, 2.4521450049289903}, vel: mgl64.Vec3{0.38157069963122026, 0, 0.19078534981561013}},
				{pos: mgl64.Vec3{4.7741159633545518, 64, 2.6370579816772759}, vel: mgl64.Vec3{0.36982595349657127, 0, 0.18491297674828563}},
			},
		},
		{
			name: "slime, friction 0.8", floor: block.Slime{},
			want: []movementStep{
				{pos: mgl64.Vec3{0.98999999999999999, 64, 0.745}, vel: mgl64.Vec3{0.48999999999999999, 0, 0.245}},
				{pos: mgl64.Vec3{1.37416, 64, 0.93708000000000002}, vel: mgl64.Vec3{0.38416, 0, 0.19208}},
				{pos: mgl64.Vec3{1.67534144, 64, 1.08767072}, vel: mgl64.Vec3{0.30118144000000002, 0, 0.15059072000000001}},
				{pos: mgl64.Vec3{1.91146768896, 64, 1.2057338444800001}, vel: mgl64.Vec3{0.23612624896000003, 0, 0.11806312448000002}},
				{pos: mgl64.Vec3{2.09659066814464, 64, 1.29829533407232}, vel: mgl64.Vec3{0.18512297918464005, 0, 0.092561489592320023}},
				{pos: mgl64.Vec3{2.2417270838253978, 64, 1.3708635419126989}, vel: mgl64.Vec3{0.1451364156807578, 0, 0.072568207840378901}},
				{pos: mgl64.Vec3{2.3555140337191118, 64, 1.4277570168595559}, vel: mgl64.Vec3{0.11378694989371411, 0, 0.056893474946857057}},
				{pos: mgl64.Vec3{2.4447230024357838, 64, 1.4723615012178919}, vel: mgl64.Vec3{0.089208968716671874, 0, 0.044604484358335937}},
				{pos: mgl64.Vec3{2.5146628339096546, 64, 1.5073314169548273}, vel: mgl64.Vec3{0.069939831473870756, 0, 0.034969915736935378}},
				{pos: mgl64.Vec3{2.5694956617851692, 64, 1.5347478308925846}, vel: mgl64.Vec3{0.054832827875514675, 0, 0.027416413937757338}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &MovementComputer{Gravity: 0.04, Drag: 0.02, DragBeforeGravity: true}
			assertMovementSequence(t, c, mgl64.Vec3{0.5, 64, 0.5}, mgl64.Vec3{0.5, 0, 0.25}, test.want, func(tx *world.Tx) {
				// A platform wide enough for the longest slide above.
				for x := -1; x <= 7; x++ {
					for z := -1; z <= 5; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, test.floor, nil)
					}
				}
			})
			if !c.OnGround() {
				t.Fatal("OnGround() after sliding along a platform = false, want true")
			}
		})
	}
}

func TestTickMovementSendsToViewersOfTheOldPosition(t *testing.T) {
	// TickMovement snapshots the viewers of the position the entity is leaving,
	// before it moves. An entity crossing into a chunk that a different viewer
	// watches still reports to the old chunk's viewers for that tick.
	w := syncWorld(t)
	v := &movementViewer{}
	start := mgl64.Vec3{0.5, 100, 0.5}
	handle := world.EntitySpawnOpts{Position: start}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		addViewer(tx, v)
		e := tx.AddEntity(handle)

		c := &MovementComputer{Gravity: 0.04, Drag: 0.02, DragBeforeGravity: true}
		m := c.TickMovement(e, start, mgl64.Vec3{}, cube.Rotation{90, 45}, tx)
		m.Send()

		if len(v.movements) != 1 {
			t.Fatalf("ViewEntityMovement calls = %d, want 1", len(v.movements))
		}
		if got := v.movements[0].pos; !got.ApproxEqualThreshold(m.Position(), physEpsilon) {
			t.Fatalf("ViewEntityMovement position = %v, want %v", got, m.Position())
		}
		if got, want := v.movements[0].rot, (cube.Rotation{90, 45}); got != want {
			t.Fatalf("ViewEntityMovement rotation = %v, want %v", got, want)
		}
		if v.movements[0].onGround {
			t.Fatal("ViewEntityMovement onGround = true for an entity falling in open air")
		}
		if len(v.velocities) != 1 {
			t.Fatalf("ViewEntityVelocity calls = %d, want 1", len(v.velocities))
		}
	})
}

func TestMovementSendThresholds(t *testing.T) {
	// PINNED BUG: the epsilon constant reads 0.001, but Send compares the deltas
	// against the zero vector and mgl64.FloatEqualThreshold squares the threshold
	// whenever either operand is zero. The real cutoff is 1e-6, a thousand times
	// tighter than the constant suggests. projectile.go:281 takes a square root to
	// compensate for exactly this, so the omission here looks unintended.
	const effective = epsilon * epsilon

	tests := []struct {
		name                          string
		dpos, dvel                    mgl64.Vec3
		wantMovements, wantVelocities int
	}{
		{name: "no change sends nothing"},
		{name: "position just below the cutoff", dpos: mgl64.Vec3{effective / 2}},
		{name: "position just above the cutoff", dpos: mgl64.Vec3{effective * 2}, wantMovements: 1},
		{name: "velocity just below the cutoff", dvel: mgl64.Vec3{0, effective / 2}},
		{name: "velocity just above the cutoff", dvel: mgl64.Vec3{0, effective * 2}, wantVelocities: 1},
		{name: "a change the named epsilon would have dropped", dpos: mgl64.Vec3{0, 0, epsilon / 2}, wantMovements: 1},
		{name: "both axes change", dpos: mgl64.Vec3{1}, dvel: mgl64.Vec3{1}, wantMovements: 1, wantVelocities: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			v := &movementViewer{}
			m := &Movement{v: []world.Viewer{v}, dpos: test.dpos, dvel: test.dvel}
			m.Send()
			if got := len(v.movements); got != test.wantMovements {
				t.Fatalf("ViewEntityMovement calls = %d, want %d", got, test.wantMovements)
			}
			if got := len(v.velocities); got != test.wantVelocities {
				t.Fatalf("ViewEntityVelocity calls = %d, want %d", got, test.wantVelocities)
			}
		})
	}
}

func TestMovementSendReachesEveryViewer(t *testing.T) {
	first, second := &movementViewer{}, &movementViewer{}
	m := &Movement{v: []world.Viewer{first, second}, dpos: mgl64.Vec3{1}, dvel: mgl64.Vec3{1}}
	m.Send()
	for i, v := range []*movementViewer{first, second} {
		if len(v.movements) != 1 || len(v.velocities) != 1 {
			t.Fatalf("viewer %d got %d movements and %d velocities, want 1 and 1", i, len(v.movements), len(v.velocities))
		}
	}
}

func TestMovementAccessorsReportPostMovementState(t *testing.T) {
	w := syncWorld(t)
	start := mgl64.Vec3{0.5, 100, 0.5}
	handle := world.EntitySpawnOpts{Position: start}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		c := &MovementComputer{Gravity: 0.04, Drag: 0.02, DragBeforeGravity: true}
		m := c.TickMovement(e, start, mgl64.Vec3{1, 0, 0}, cube.Rotation{10, 20}, tx)

		if got, want := m.Position(), (mgl64.Vec3{1.48, 99.96, 0.5}); !got.ApproxEqualThreshold(want, physEpsilon) {
			t.Fatalf("Position() = %v, want %v", got, want)
		}
		if got, want := m.Velocity(), (mgl64.Vec3{0.98, -0.04, 0}); !got.ApproxEqualThreshold(want, physEpsilon) {
			t.Fatalf("Velocity() = %v, want %v", got, want)
		}
		// TickMovement passes rotation straight through: MovementComputer never
		// derives a rotation from velocity, unlike ProjectileBehaviour.
		if got, want := m.Rotation(), (cube.Rotation{10, 20}); got != want {
			t.Fatalf("Rotation() = %v, want %v", got, want)
		}
		// TickMovement does not write the entity's own data; the caller does.
		if got := e.Position(); !got.ApproxEqual(start) {
			t.Fatalf("entity position after TickMovement = %v, want %v unchanged", got, start)
		}
	})
}

func TestTickMovementDeltaVelocityIsMeasuredBeforeForces(t *testing.T) {
	// dvel compares the velocity after collision against the velocity handed in,
	// so a tick that only loses speed to drag still counts as a velocity change.
	w := syncWorld(t)
	start := mgl64.Vec3{0.5, 100, 0.5}
	handle := world.EntitySpawnOpts{Position: start}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		c := &MovementComputer{Drag: 0.02}
		m := c.TickMovement(e, start, mgl64.Vec3{1, 0, 0}, cube.Rotation{}, tx)
		if got, want := m.dvel, (mgl64.Vec3{-0.02, 0, 0}); !got.ApproxEqualThreshold(want, physEpsilon) {
			t.Fatalf("dvel = %v, want %v", got, want)
		}
		if got, want := m.dpos, (mgl64.Vec3{0.98, 0, 0}); !got.ApproxEqualThreshold(want, physEpsilon) {
			t.Fatalf("dpos = %v, want %v", got, want)
		}
	})
}

func TestTickMovementNaNVelocity(t *testing.T) {
	// PINNED BUG: nothing rejects a NaN velocity. It reaches blockBBoxsAround,
	// where int(math.Floor(NaN)) and int(math.Ceil(NaN)) collapse to the same
	// value, so the search loop runs zero times and no block is collected at all
	// -- collision is silently disabled on every axis, not just the poisoned one.
	// The NaN then survives every comparison in CheckCollision and lands in the
	// entity position.
	w := syncWorld(t)
	start := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: start}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		// A wall the entity would be stopped by if the block search still ran.
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		e := tx.AddEntity(handle)

		c := &MovementComputer{}
		dPos, _ := c.CheckCollision(tx, e, start, mgl64.Vec3{math.NaN(), 0, 0.5})
		if !math.IsNaN(dPos[0]) {
			t.Fatalf("delta X = %v, want NaN carried through unguarded", dPos[0])
		}
		if dPos[2] != 0.5 {
			t.Fatalf("delta Z = %v, want the full 0.5: a NaN on X empties the block search for every axis", dPos[2])
		}
	})
}

func TestBlockBBoxsAroundInfiniteVelocityDoesNotTerminate(t *testing.T) {
	// PINNED BUG: an infinite velocity component makes blockBBoxsAround iterate
	// from math.MinInt64 to a finite bound, which does not finish in any useful
	// time. Any code path that lets a plugin or a packet set an infinite velocity
	// therefore hangs the world goroutine. This test pins the arithmetic that
	// causes it rather than calling the function, because calling it would never
	// return.
	box := cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125).
		Translate(mgl64.Vec3{0.5, 64, 0.5}).
		Extend(mgl64.Vec3{0, math.Inf(-1), 0}).
		Grow(0.25)

	minY, maxY := int(math.Floor(box.Min()[1])), int(math.Ceil(box.Max()[1]))
	if minY != math.MinInt64 {
		t.Fatalf("loop start = %d, want math.MinInt64 for an infinite lower bound", minY)
	}
	if maxY <= minY {
		t.Fatalf("loop bounds = [%d, %d), want a range blockBBoxsAround would walk one block at a time", minY, maxY)
	}
}

func TestTickMovementBelowWorldMinimum(t *testing.T) {
	// MovementComputer has no notion of the world's vertical range: it keeps
	// falling past the bottom. Removing the entity is Ent.Tick's job.
	w := syncWorld(t)
	var floor float64
	mustDo(t, w, func(tx *world.Tx) {
		floor = float64(tx.Range()[0])
	})
	start := mgl64.Vec3{0.5, floor + 0.5, 0.5}
	handle := world.EntitySpawnOpts{Position: start}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		c := &MovementComputer{Gravity: 0.04, Drag: 0.02, DragBeforeGravity: true}
		pos, vel := start, mgl64.Vec3{}
		for range 20 {
			m := c.TickMovement(e, pos, vel, cube.Rotation{}, tx)
			pos, vel = m.Position(), m.Velocity()
		}
		if pos[1] >= floor {
			t.Fatalf("position after 20 ticks = %v, want a fall below the world minimum %v", pos[1], floor)
		}
		if c.OnGround() {
			t.Fatal("OnGround() below the world minimum = true, want false")
		}
	})
}

// assertMovementSequence runs len(want) ticks of c against a fresh Synchronous
// world and compares every position and velocity to the golden step. build, if
// non-nil, places blocks before the first tick.
func assertMovementSequence(t *testing.T, c *MovementComputer, pos, vel mgl64.Vec3, want []movementStep, build func(tx *world.Tx)) {
	t.Helper()
	w := syncWorld(t)
	handle := world.EntitySpawnOpts{Position: pos}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		if build != nil {
			build(tx)
		}
		e := tx.AddEntity(handle)
		for i, step := range want {
			m := c.TickMovement(e, pos, vel, cube.Rotation{}, tx)
			pos, vel = m.Position(), m.Velocity()
			assertVec3(t, fmt.Sprintf("tick %d position", i+1), pos, step.pos)
			assertVec3(t, fmt.Sprintf("tick %d velocity", i+1), vel, step.vel)
		}
	})
}

func assertVec3(t *testing.T, name string, got, want mgl64.Vec3) {
	t.Helper()
	for axis := range 3 {
		if math.Abs(got[axis]-want[axis]) > physEpsilon {
			t.Fatalf("%s = {%.17g, %.17g, %.17g}, want {%.17g, %.17g, %.17g}",
				name, got[0], got[1], got[2], want[0], want[1], want[2])
		}
	}
}

func finite(v mgl64.Vec3) bool {
	for axis := range 3 {
		if math.IsNaN(v[axis]) || math.IsInf(v[axis], 0) {
			return false
		}
	}
	return true
}

// syncWorld returns a Synchronous world that is closed when the test ends.
// Synchronous worlds load chunks on the calling goroutine and only tick on
// AdvanceTick, so every test using one is deterministic.
func syncWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() {
		_ = w.Close()
	})
	return w
}

// addViewer registers v as a viewer of the chunks around the origin, so that
// tx.Viewers returns it for positions in those chunks.
func addViewer(tx *world.Tx, v world.Viewer) {
	l := world.NewLoader(2, tx.World(), v)
	l.Load(tx, 32)
}

// movementViewer records the entity updates a Movement sends.
type movementViewer struct {
	world.NopViewer

	movements []struct {
		pos      mgl64.Vec3
		rot      cube.Rotation
		onGround bool
	}
	velocities []mgl64.Vec3
}

func (v *movementViewer) ViewEntityMovement(_ world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	v.movements = append(v.movements, struct {
		pos      mgl64.Vec3
		rot      cube.Rotation
		onGround bool
	}{pos, rot, onGround})
}

func (v *movementViewer) ViewEntityVelocity(_ world.Entity, vel mgl64.Vec3) {
	v.velocities = append(v.velocities, vel)
}
