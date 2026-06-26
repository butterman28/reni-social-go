package loader

import (
    "os"
    "gopkg.in/yaml.v3"
    "reni-social/schema/types"
)

func LoadTable(path string) (*types.Table, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    var table types.Table
    err = yaml.Unmarshal(data, &table)
    if err != nil {
        return nil, err
    }

    return &table, nil
}