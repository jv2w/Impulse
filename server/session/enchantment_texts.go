package session

// enchantNames is the pool of words shown on the enchanting table's option buttons. The client renders
// them in the Standard Galactic Alphabet, so they are decorative: the word carries no information about
// the enchantment that is actually applied. The pool matches the vocabulary vanilla draws from.
var enchantNames = []string{
	"the", "elder", "scrolls", "klaatu", "berata", "niktu", "xyzzy", "bless", "curse", "light",
	"darkness", "fire", "air", "earth", "water", "hot", "dry", "cold", "wet", "ignite",
	"snuff", "embiggen", "twist", "shorten", "stretch", "fiddle", "destroy", "imbue", "galvanize",
	"enchant", "free", "limited", "range", "of", "towards", "inside", "sphere", "cube", "self",
	"other", "ball", "mental", "void", "travel", "summon", "banish", "expose", "heal", "decay",
	"wither", "murder", "kill", "vanish", "unmake", "undo", "hurt", "harm", "restore", "vitalize",
	"reduce", "enlarge", "phase", "sanctify", "corrupt", "shatter", "mend", "quicken", "still",
	"bind", "sever", "cloak", "reveal", "warp", "anchor", "drift", "kindle", "quench", "temper",
}
