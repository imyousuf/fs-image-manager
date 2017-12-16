package services

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Device represents the device browsed from
type Device struct {
	deviceID               string
	currentCookieValue     string
	currentCookieValidTill time.Time
	createdAt              time.Time
	lastUpdatedAt          time.Time
}

// GetDeviceID retrieves the ID of the Device
func (device *Device) GetDeviceID() string {
	return device.deviceID
}

// GetCurrentCookieValue retrieves the cookie value of the device
func (device *Device) GetCurrentCookieValue() string {
	return device.currentCookieValue
}

// GetCurrentCookieValidTill retrieves till when the cookie is valid up to
func (device *Device) GetCurrentCookieValidTill() time.Time {
	return device.currentCookieValidTill
}

// IsDeviceCookieStillValid retrieves whether the cookie still valid
func (device *Device) IsDeviceCookieStillValid() bool {
	return time.Now().AddDate(-9, -11, 0).After(device.currentCookieValidTill)
}

// CreatedAt returns when this device was first registered with the system
func (device *Device) CreatedAt() time.Time {
	return device.createdAt
}

func (device *Device) UpdatedAt() time.Time {
	return device.lastUpdatedAt
}

// UpdateDeviceWithNewCookie updates the cookie of the device
func (device *Device) UpdateDeviceWithNewCookie() error {
	id, _ := uuid.NewRandom()
	device.currentCookieValue = id.String()
	device.currentCookieValidTill = time.Now().AddDate(10, 0, 0)
	return updateDevice(device)
}

// CreateDevice creates a new device
func CreateDevice() *Device {
	device := &Device{}
	id, _ := uuid.NewRandom()
	device.deviceID = id.String()
	id, _ = uuid.NewRandom()
	device.currentCookieValue = id.String()
	device.currentCookieValidTill = time.Now().AddDate(10, 0, 0)
	saveDevice(device)
	return device
}

func saveDevice(device *Device) error {
	deviceModel := &DeviceModel{}
	deviceModel.DeviceID = device.deviceID
	deviceModel.CurrentCookieValue = device.currentCookieValue
	deviceModel.CurrentCookieValidTill = device.currentCookieValidTill
	saveResult := GetDB().Save(deviceModel)
	errors := saveResult.GetErrors()
	if len(errors) > 0 {
		return errors[0]
	}
	device.createdAt = deviceModel.CreatedAt
	device.lastUpdatedAt = deviceModel.UpdatedAt
	return nil
}

func updateDevice(device *Device) error {
	deviceModel := &DeviceModel{}
	GetDB().Where(&DeviceModel{DeviceID: device.GetDeviceID()}).First(deviceModel)
	if GetDB().NewRecord(deviceModel) {
		return errors.New("Trying to update device that is non-existent: " + device.GetDeviceID())
	}
	deviceModel.CurrentCookieValue = device.currentCookieValue
	deviceModel.CurrentCookieValidTill = device.currentCookieValidTill
	saveResult := GetDB().Save(deviceModel)
	errors := saveResult.GetErrors()
	if len(errors) > 0 {
		return errors[0]
	}
	device.lastUpdatedAt = deviceModel.UpdatedAt
	return nil
}

// GetDevice retrieves the device for which
func GetDevice(cookieValue string) (*Device, bool) {
	deviceModel := &DeviceModel{}
	GetDB().Where(&DeviceModel{CurrentCookieValue: cookieValue}).First(deviceModel)
	return getDeviceModelToDevice(deviceModel), !GetDB().NewRecord(deviceModel)
}

func getDeviceModelToDevice(deviceModel *DeviceModel) *Device {
	device := &Device{}
	device.createdAt = deviceModel.CreatedAt
	device.lastUpdatedAt = deviceModel.UpdatedAt
	device.deviceID = deviceModel.DeviceID
	device.currentCookieValue = deviceModel.CurrentCookieValue
	device.currentCookieValidTill = deviceModel.CurrentCookieValidTill
	return device
}
