package status

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/byuoitav/av-api/base"
	"github.com/byuoitav/av-api/dbo"
	"github.com/byuoitav/configuration-database-microservice/accessors"
	"github.com/byuoitav/event-router-microservice/eventinfrastructure"
)

func GetRoomStatus(building string, roomName string) (base.PublicRoom, error) {

	room, err := dbo.GetRoomByInfo(building, roomName)
	if err != nil {
		return base.PublicRoom{}, err
	}

	commands, err := generateStatusCommands(room, DEFAULT_MAP)
	if err != nil {
		return base.PublicRoom{}, err
	}

	responses, err := runStatusCommands(commands)
	if err != nil {
		return base.PublicRoom{}, err
	}

	roomStatus, err := evaluateResponses(responses)
	if err != nil {
		return base.PublicRoom{}, err
	}

	roomStatus.Building = building
	roomStatus.Room = roomName

	return roomStatus, nil
}

func generateStatusCommands(room accessors.Room, commandMap map[string]StatusEvaluator) ([]StatusCommand, error) {

	log.Printf("Generating commands...")

	var output []StatusCommand

	for _, possibleEvaluator := range room.Configuration.Evaluators {

		if strings.HasPrefix(possibleEvaluator.EvaluatorKey, FLAG) {

			currentEvaluator := DEFAULT_MAP[possibleEvaluator.EvaluatorKey]

			devices, err := currentEvaluator.GetDevices(room)
			if err != nil {
				return []StatusCommand{}, err
			}

			commands, err := currentEvaluator.GenerateCommands(devices)
			if err != nil {
				return []StatusCommand{}, err
			}

			output = append(output, commands...)
		}
	}

	return output, nil
}

func runStatusCommands(commands []StatusCommand) (outputs []Status, err error) {

	log.Printf("Running commands...")

	if len(commands) == 0 {
		err = errors.New("No commands")
		return
	}

	//map device names to commands
	commandMap := make(map[string][]StatusCommand)

	log.Printf("Building device map")
	for _, command := range commands {

		_, present := commandMap[command.Device.Name]
		if !present {
			commandMap[command.Device.Name] = []StatusCommand{command}
			log.Printf("Device %s identified", command.Device.Name)
		} else {
			commandMap[command.Device.Name] = append(commandMap[command.Device.Name], command)
		}

	}

	log.Printf("Creating channel")
	channel := make(chan Status, len(commandMap))
	var group sync.WaitGroup

	for _, deviceCommands := range commandMap {
		group.Add(1)
		go issueCommands(deviceCommands, channel, &group)
	}

	log.Printf("Waiting for commands to issue...")
	group.Wait()
	log.Printf("Done. Closing channel...")
	close(channel)

	for output := range channel {
		if output.ErrorMessage != nil {
			log.Printf("Error querying status with destination: %s", output.DestinationDevice.Name)
			cause := eventinfrastructure.INTERNAL
			message := *output.ErrorMessage
			message = "Error querying status for destination" + output.DestinationDevice.Name + ":" + message
			base.PublishError(message, cause)
		}
		log.Printf("Appending results of %s to output", output.DestinationDevice.Name)
		outputs = append(outputs, output)
	}
	return
}

//builds a Status object corresponding to a device and writes it to the channel
func issueCommands(commands []StatusCommand, channel chan Status, control *sync.WaitGroup) {

	//add task to waitgroup

	//final output
	output := Status{DestinationDevice: commands[0].DestinationDevice}
	statuses := []StatusResponse{}

	//iterate over list of StatusCommands
	//TODO:make sure devices can handle rapid-fire API requests
	for _, command := range commands {
		statusResponseMap := make(map[string]interface{})

		//build url
		url := command.Action.Microservice + command.Action.Endpoint.Path
		for formal, actual := range command.Parameters {
			toReplace := ":" + formal
			if !strings.Contains(url, toReplace) {
				errorMessage := "Could not find parameter " + toReplace + " issuing the command " + command.Action.Name
				output.ErrorMessage = &errorMessage
				log.Printf(errorMessage)
			} else {
				url = strings.Replace(url, toReplace, actual, -1)
			}
		}

		log.Printf("Sending requqest to %s", url)
		response, err := http.Get(url)
		if err != nil {
			errorMessage := err.Error()
			output.ErrorMessage = &errorMessage
			log.Printf("Error getting response from %s", command.Device.Name)
			continue
		}
		defer response.Body.Close()

		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			errorMessage := err.Error()
			output.ErrorMessage = &errorMessage
			log.Printf("Error reading response from %s", command.Device.Name)
			continue
		}
		log.Printf("Microservice returned: %s", body)

		var status map[string]interface{}
		err = json.Unmarshal(body, &status)
		if err != nil {
			errorMessage := err.Error()
			output.ErrorMessage = &errorMessage
			log.Printf("Error unmarshalling response from %s", command.Device.Name)
			continue
		}

		log.Printf("Copying data into output")
		for device, object := range status {
			statusResponseMap[device] = object
			log.Printf("%s maps to %v", device, object)
		}
		//add the full status response
		statuses = append(statuses, StatusResponse{Generator: command.Generator, Status: statusResponseMap})
	}

	//set the map of statuses to output
	output.Responses = statuses
	//write output to channel
	log.Printf("Writing output to channel...")
	for key, value := range output.Status {
		log.Printf("%s maps to %v", key, value)
	}

	channel <- output
	log.Printf("Done acquiring status of %s", output.DestinationDevice.Name)
	control.Done()
}

func evaluateResponses(responses []Status) (base.PublicRoom, error) {

	log.Printf("Evaluating responses...")

	var AudioDevices []base.AudioDevice
	var Displays []base.Display

	for _, device := range responses {
		newMap := make(map[string]interface{})
		//as the first step, we're gonna run it through processing, to allow the evaluator a chance to
		//tweak the values.
		for _, response := range device.Responses {
			for key, value := range response.Status {
				k, v, err := DEFAULT_MAP[response.Generator].EvaluateResponse(key, value)
				if err != nil {
					//log an error
					log.Printf("There was a problem procesing the response %v - %v with evaluator %v: %s",
						key, value, response.Generator, err.Error())
					continue
				}
				newMap[k] = v
			}
			device.Status = newMap
		}

		if device.DestinationDevice.AudioDevice {
			audioDevice, err := processAudioDevice(device)
			if err == nil {
				AudioDevices = append(AudioDevices, audioDevice)
			}
		}

		if device.DestinationDevice.Display {

			display, err := processDisplay(device)
			if err == nil {
				Displays = append(Displays, display)
			}
		}

	}

	return base.PublicRoom{Displays: Displays, AudioDevices: AudioDevices}, nil
}

func processAudioDevice(device Status) (base.AudioDevice, error) {

	log.Printf("Adding audio device: %s", device.DestinationDevice.Name)
	log.Printf("Status map: %v", device.Status)

	var audioDevice base.AudioDevice

	muted, ok := device.Status["muted"]
	mutedBool, ok := muted.(bool)
	if ok {
		audioDevice.Muted = &mutedBool
	}

	volume, ok := device.Status["volume"]
	if ok {
		//Default unmarshals to a float 64 - so we have to coerce it to an int
		var volumeInt int
		if volFloat, ok := volume.(float64); ok {
			volumeInt = int(volFloat)
		} else {
			volumeInt, ok = volume.(int)
		}

		//volumeint should be set now
		if ok {
			audioDevice.Volume = &volumeInt
		} else {
			log.Printf("Volume type assertion failed for %v", volume)
		}
	}

	power, ok := device.Status["power"]
	powerString, ok := power.(string)
	if ok {
		audioDevice.Power = powerString
	}

	input, ok := device.Status["input"]
	inputString, ok := input.(string)
	if ok {
		audioDevice.Input = inputString
	}

	audioDevice.Name = device.DestinationDevice.Name
	return audioDevice, nil
}

func processDisplay(device Status) (base.Display, error) {

	log.Printf("Adding display: %s", device.DestinationDevice.Name)

	var display base.Display

	blanked, ok := device.Status["blanked"]
	blankedBool, ok := blanked.(bool)
	if ok {
		display.Blanked = &blankedBool
	}

	power, ok := device.Status["power"]
	powerString, ok := power.(string)
	if ok {
		display.Power = powerString
	}

	input, ok := device.Status["input"]
	inputString, ok := input.(string)
	if ok {
		display.Input = inputString
	}

	display.Name = device.DestinationDevice.Name

	return display, nil
}

func generateStandardStatusCommand(devices []accessors.Device, evaluatorName string, commandName string) ([]StatusCommand, error) {
	log.Printf("Generating status commands from %v", evaluatorName)
	var output []StatusCommand

	//iterate over each device
	for _, device := range devices {

		log.Printf("Considering device: %s", device.Name)

		for _, command := range device.Commands {
			if strings.HasPrefix(command.Name, FLAG) && strings.Contains(command.Name, commandName) {
				log.Printf("Command found")

				//every power command needs an address parameter
				parameters := make(map[string]string)
				parameters["address"] = device.Address

				//build destination device
				var destinationDevice DestinationDevice
				for _, role := range device.Roles {

					if role == "AudioOut" {
						destinationDevice.AudioDevice = true
					}

					if role == "VideoOut" {
						destinationDevice.Display = true
					}

				}
				destinationDevice.Device = device

				log.Printf("Adding command: %s to action list with device %s", command.Name, device.Name)
				output = append(output, StatusCommand{
					Action:            command,
					Device:            device,
					Parameters:        parameters,
					DestinationDevice: destinationDevice,
					Generator:         evaluatorName,
				})

			}

		}

	}
	return output, nil

}
