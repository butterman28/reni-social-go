package types

type References struct {
	Table    string `yaml:"table"`
	Column   string `yaml:"column"`
	OnDelete string `yaml:"on_delete"`
}

type Column struct {
	Type       string `yaml:"type"`
	NotNull    bool   `yaml:"not_null"`
	Unique     bool   `yaml:"unique"`
	Default    string `yaml:"default"`
	PrimaryKey bool   `yaml:"primary_key"`
	Drop       bool   `yaml:"drop"`

	References References `yaml:"references"`
}