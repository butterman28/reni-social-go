package types

type Table struct {
    Table       string            `yaml:"table"`
    IfNotExists bool              `yaml:"if_not_exists"`
    Columns     map[string]Column `yaml:"columns"`
    Indexes     []Index           `yaml:"indexes"`
}



type Index struct {
    Name    string   `yaml:"name"`
    Columns []string `yaml:"columns"`
    Unique  bool     `yaml:"unique"`
}