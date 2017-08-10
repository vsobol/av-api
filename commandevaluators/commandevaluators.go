package commandevaluators

import (
	"encoding/json"

	"github.com/byuoitav/av-api/base"
	"github.com/byuoitav/av-api/dbo"
	"github.com/byuoitav/configuration-database-microservice/accessors"
)

//CommandExecutionReporting is a struct we use to keep track of command execution
//for reporting to the user.
type CommandExecutionReporting struct {
	Success bool   `json:"success"`
	Action  string `json:"action"`
	Device  string `json:"device"`
	Err     string `json:"error,omitempty"`
}

/*
CommandEvaluator is an interface that must be implemented for each command to be
evaluated.
*/
type CommandEvaluator interface {
	/*
		 	Evalute takes a public room struct, scans the struct and builds any needed
			actions based on the contents of the struct.
	*/
	Evaluate(base.PublicRoom) ([]base.ActionStructure, error)
	/*
		  Validate takes an action structure (for the command) and validates
			that the device and parameter are valid for the command.
	*/
	Validate(base.ActionStructure) error
	/*
			   GetIncompatableActions returns a list of commands that are incompatable
		     with this one (i.e. 'standby' and 'power on', or 'mute' and 'volume up')
	*/
	GetIncompatibleCommands() []string
}

//CommandMap is a singleton that
//maps known commands to their evaluation structure. init will return a pointer to this.
var CommandMap = make(map[string]CommandEvaluator)
var commandMapInitialized = false

func getDevice(devs []accessors.Device, d string, room string, building string) (dev accessors.Device, err error) {
	for i, curDevice := range devs {
		if checkDevicesEqual(curDevice, d, room, building) {
			dev = devs[i]
			return
		}
	}
	var device accessors.Device

	device, err = dbo.GetDeviceByName(building, room, d)
	if err != nil {
		return
	}
	dev = device
	return
}

func getKeyValueFromCommmand(action base.ActionStructure) []string {
	switch action.Action {
	case "PowerOn":
		return []string{"power", "on"}
	case "Standby":
		return []string{"power", "standby"}
	case "ChangeInput":
		b, _ := json.Marshal(action.Parameters)
		return []string{"input", string(b)}
	case "SetVolume":
		return []string{"volume", action.Parameters["level"]}
	case "BlankDisplay":
		return []string{"blanked", "true"}
	case "UnblankDisplay":
		return []string{"blanked", "false"}
	case "Mute":
		return []string{"Muted", "true"}
	case "UnMute":
		return []string{"Muted", "false"}
	}
	return []string{}
}

//soft singleton command map
func Init() map[string]CommandEvaluator {
	if !commandMapInitialized {
		CommandMap["PowerOnDefault"] = &PowerOnDefault{}
		CommandMap["StandbyDefault"] = &StandbyDefault{}
		CommandMap["ChangeVideoInputDefault"] = &ChangeVideoInputDefault{}
		CommandMap["ChangeAudioInputDefault"] = &ChangeAudioInputDefault{}
		CommandMap["ChangeVideoInputVideoSwitcher"] = &ChangeVideoInputVideoSwitcher{}
		CommandMap["BlankDisplayDefault"] = &BlankDisplayDefault{}
		CommandMap["UnBlankDisplayDefault"] = &UnBlankDisplayDefault{}
		CommandMap["MuteDefault"] = &MuteDefault{}
		CommandMap["UnMuteDefault"] = &UnMuteDefault{}
		CommandMap["SetVolumeDefault"] = &SetVolumeDefault{}
		CommandMap["SetVolumeDMPS"] = &SetVolumeDMPS{}
		CommandMap["SetVolumeTecLite"] = &SetVolumeTecLite{}
		CommandMap["ChangeVideoInputDMPS"] = &ChangeVideoInputDMPS{}
		CommandMap["MuteDSP"] = &MuteDSP{}
		CommandMap["UnmuteDSP"] = &UnMuteDSP{}
		CommandMap["SetVolumeDSP"] = &SetVolumeDSP{}

		commandMapInitialized = true
	}

	return CommandMap
}
