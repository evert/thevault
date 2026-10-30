package world

// EnemyType is what an enemy looks like on the map. Every type has its own
// lowercase letter (players are uppercase) and colour.
type EnemyType struct {
	Name  string
	Glyph string
	Color int // 256-colour palette index
}

// Tiers of enemy types, one per floor; they get nastier the deeper you go.
var Tiers = [Floors][]EnemyType{
	{
		{"Deprecation Warning", "d", 179},
		{"Stale Lockfile", "s", 108},
		{"Unused Import", "u", 152},
		{"Flaky Test", "f", 156},
		{"Lint Gremlin", "g", 113},
	},
	{
		{"Phantom Dependency", "p", 189},
		{"Peer Dep Wraith", "w", 147},
		{"Semver Liar", "l", 215},
		{"Bloated Bundle", "b", 180},
		{"Orphaned Fork", "o", 109},
	},
	{
		{"Typosquatter", "t", 220},
		{"Circular Import", "c", 81},
		{"Abandonware Golem", "a", 137},
		{"Postinstall Imp", "i", 170},
		{"Version Drift", "v", 79},
	},
	{
		{"Dependency Hell", "h", 202},
		{"Breaking Change", "x", 160},
		{"Merge Conflict Hydra", "m", 165},
		{"Leaky Abstraction", "e", 39},
		{"Diamond Dependency", "n", 87},
	},
	{
		{"Supply Chain Worm", "q", 196},
		{"Zero-Day Lurker", "z", 199},
		{"Malicious Maintainer", "k", 124},
		{"Protestware Shade", "r", 93},
		{"Yanked Release", "y", 166},
	},
}

var Boss = EnemyType{"The node_modules Abyss", "Ω", 201}

var typesByName = func() map[string]EnemyType {
	m := map[string]EnemyType{Boss.Name: Boss}
	for _, tier := range Tiers {
		for _, t := range tier {
			m[t.Name] = t
		}
	}
	return m
}()

// TypeOf returns an enemy's type by its name.
func TypeOf(name string) EnemyType {
	if t, ok := typesByName[name]; ok {
		return t
	}
	return EnemyType{Name: name, Glyph: "&", Color: 197}
}
