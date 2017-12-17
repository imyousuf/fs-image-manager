package services

import (
	"time"

	"github.com/jinzhu/gorm"
)

// DeviceModel represents the DB state of short code to URL mapping
type DeviceModel struct {
	gorm.Model
	DeviceID               string `gorm:"not null;unique"`
	Name                   string
	CurrentCookieValue     string `gorm:"not null;unique"`
	CurrentCookieValidTill time.Time
	Downloads              []DownloadHistoryModel `gorm:"ForeignKey:DeviceID;AssociationForeignKey:DeviceID"`
}

// DownloadHistoryModel tracks a download was performed on a device
type DownloadHistoryModel struct {
	gorm.Model
	DeviceID            string
	DownloadRequestedAt time.Time
	Files               []DownloadedFileModel `gorm:"ForeignKey:DownloadID"`
}

// DownloadedFileModel file that was downloaded
type DownloadedFileModel struct {
	gorm.Model
	FilePath   string
	DownloadID uint
}
