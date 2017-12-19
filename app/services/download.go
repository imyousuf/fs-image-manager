package services

import (
	"log"
	"time"
)

// DownloadHistory has all the information regarding the download
type DownloadHistory struct {
	downloadPerformedAt time.Time
	deviceID            string
	id                  uint
}

// GetDownloadPerformedAt retrieves the time when the download was performed
func (history *DownloadHistory) GetDownloadPerformedAt() time.Time {
	return history.downloadPerformedAt
}

// GetDevice returns the device from where download request was initiated
func (history *DownloadHistory) GetDevice() (*Device, error) {
	deviceModel, _, err := getDeviceModelByDeviceID(history.deviceID)
	if err != nil {
		return new(Device), err
	}
	return getDeviceModelToDevice(deviceModel), nil
}

// GetDownloadedFiles returns the list of files that was downloaded
func (history *DownloadHistory) GetDownloadedFiles() []string {
	historyModel := new(DownloadHistoryModel)
	queryOutcome := GetDB().Where(history.id).First(historyModel)
	if queryOutcome.Error != nil {
		log.Println(queryOutcome.Error)
		return make([]string, 0, 0)
	}
	filteredFilesQuery := GetDB().Where(&DownloadedFileModel{DownloadID: historyModel.ID})
	downloadedFileModels := make([]DownloadedFileModel, 0, 0)
	filteredFilesQuery.Find(&downloadedFileModels)
	filePaths := make([]string, 0, len(downloadedFileModels))
	for _, dowloadedFile := range downloadedFileModels {
		filePaths = append(filePaths, dowloadedFile.FilePath)
	}
	return filePaths
}

// GetDownloadID returns the ID of this download
func (history *DownloadHistory) GetDownloadID() uint {
	return history.id
}

func getDownloadHistoryFromModel(downloadHistoryModel *DownloadHistoryModel) *DownloadHistory {
	return &DownloadHistory{deviceID: downloadHistoryModel.DeviceID,
		downloadPerformedAt: downloadHistoryModel.DownloadRequestedAt, id: downloadHistoryModel.ID}
}

func recordDownload(device *Device, relativePaths []string) (*DownloadHistory, error) {
	model := &DownloadHistoryModel{DeviceID: device.deviceID, DownloadRequestedAt: time.Now()}
	log.Println("Record downloading for", device.GetDeviceID(), relativePaths)
	res := GetDB().Save(model)
	if res.Error != nil {
		log.Println(res.Error.Error())
		return new(DownloadHistory), res.Error
	}
	for _, path := range relativePaths {
		downloadFileModel := &DownloadedFileModel{FilePath: path, DownloadID: model.ID}
		res = GetDB().Save(downloadFileModel)
		if res.Error != nil {
			log.Println(res.Error.Error())
		} else {
			log.Println(downloadFileModel)
		}
	}
	return getDownloadHistoryFromModel(model), nil
}
