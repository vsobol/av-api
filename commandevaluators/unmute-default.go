package commandevaluators

import (
	"errors"
	"log"
	"strings"

	"github.com/byuoitav/av-api/base"
	"github.com/byuoitav/av-api/dbo"
	"github.com/byuoitav/event-router-microservice/eventinfrastructure"
)

type UnMuteDefault struct {
}

func (p *UnMuteDefault) Evaluate(room base.PublicRoom) ([]base.ActionStructure, error) {
	log.Printf("Evaluating UnMute command.")

	var actions []base.ActionStructure
	eventInfo := eventinfrastructure.EventInfo{
		Type:           eventinfrastructure.USERACTION,
		EventCause:     eventinfrastructure.USERINPUT,
		EventInfoKey:   "muted",
		EventInfoValue: "false",
	}

	//check if request is a roomwide unmute
	if room.Muted != nil && !*room.Muted {

		log.Printf("Room-wide UnMute request recieved. Retrieving all devices")

		devices, err := dbo.GetDevicesByBuildingAndRoomAndRole(room.Building, room.Room, "AudioOut")
		if err != nil {
			return []base.ActionStructure{}, err
		}

		log.Printf("UnMuting alll devices in room.")

		for _, device := range devices {

			if device.Output {

				log.Printf("Adding device %+v", device.Name)

				eventInfo.Device = device.Name
				actions = append(actions, base.ActionStructure{
					Action:              "UnMute",
					GeneratingEvaluator: "UnMuteDefault",
					Device:              device,
					DeviceSpecific:      false,
					EventLog:            []eventinfrastructure.EventInfo{eventInfo},
				})

			}

		}

	}

	//check specific devices
	log.Printf("Evaluating individual audio devices for unmuting.")

	for _, audioDevice := range room.AudioDevices {

		log.Printf("Adding device %+v", audioDevice.Name)

		if audioDevice.Muted != nil && !*audioDevice.Muted {

			device, err := dbo.GetDeviceByName(room.Building, room.Room, audioDevice.Name)
			if err != nil {
				return []base.ActionStructure{}, err
			}

			eventInfo.Device = device.Name
			actions = append(actions, base.ActionStructure{
				Action:              "UnMute",
				GeneratingEvaluator: "UnMuteDefault",
				Device:              device,
				DeviceSpecific:      true,
				EventLog:            []eventinfrastructure.EventInfo{eventInfo},
			})

		}

	}

	log.Printf("%v actions generated.", len(actions))
	log.Printf("Evalutation complete.")

	return actions, nil

}

func (p *UnMuteDefault) Validate(action base.ActionStructure) error {

	log.Printf("Validating action for command \"UnMute\"")

	ok, _ := CheckCommands(action.Device.Commands, "UnMute")

	if !ok || !strings.EqualFold(action.Action, "UnMute") {
		log.Printf("ERROR. %s is an invalid command for %s", action.Action, action.Device.Name)
		return errors.New(action.Action + " is an invalid command for" + action.Device.Name)
	}

	log.Printf("Done.")
	return nil
}

func (p *UnMuteDefault) GetIncompatibleCommands() (incompatibleActions []string) {

	incompatibleActions = []string{
		"Mute",
	}

	return
}
