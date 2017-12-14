package services

import (
	"time"

	"github.com/jinzhu/gorm"
)

// DeviceModel represents the DB state of short code to URL mapping
type DeviceModel struct {
	gorm.Model
	DeviceID               string `gorm:"not null;unique"`
	CurrentCookieValue     string `gorm:"not null;unique"`
	CurrentCookieValidTill time.Time
	Downloads              []DownloadHistory `gorm:"ForeignKey:DeviceID;AssociationForeignKey:DeviceID"`
}

// DownloadHistory tracks a download was performed on a device
type DownloadHistory struct {
	gorm.Model
	DeviceID            string
	DownloadRequestedAt time.Time
	Files               []DownloadedFile `gorm:"ForeignKey:DownloadID"`
}

// DownloadedFile file that was downloaded
type DownloadedFile struct {
	gorm.Model
	FilePath   string
	DownloadID uint
}
