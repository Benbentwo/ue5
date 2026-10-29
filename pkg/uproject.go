package pkg

import (
	"encoding/json"
	"os"
)

type Uproject struct {
	FileVersion       int    `json:"FileVersion"`
	EngineAssociation string `json:"EngineAssociation"`
	Category          string `json:"Category"`
	Description       string `json:"Description"`
	Modules           []struct {
		Name         string `json:"Name"`
		Type         string `json:"Type"`
		LoadingPhase string `json:"LoadingPhase"`
	} `json:"Modules"`
	Plugins []struct {
		Name            string   `json:"Name"`
		Enabled         bool     `json:"Enabled"`
		TargetAllowList []string `json:"TargetAllowList"`
	} `json:"Plugins"`
}

func NewUprojectE(path string) (*Uproject, error) {
	var uProject Uproject
	bytes, err := os.ReadFile(path) // Read the file content
	if err != nil {
		return nil, err // Return error if file reading fails
	}
	err = json.Unmarshal(bytes, &uProject)
	if err != nil {
		return nil, err
	} // Unmarshal JSON data into Uproject struct

	return &uProject, nil // Return the populated Uproject struct
}

func NewUproject(path string) *Uproject {
	uProject, err := NewUprojectE(path)
	if err != nil {
		panic(err) // Panic if there is an error reading or parsing the file
	}
	return uProject // Return the populated Uproject struct
}
