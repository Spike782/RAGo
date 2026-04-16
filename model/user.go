package model

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        int64          `gorm:"primaryKey" json:"id"`
	Name      string         `gorm:"type:varchar(50)" json:"name"`
	Email     string         `gorm:"type:varchar(100);index" json:"email"`
	Username  string         `gorm:"type:varchar(50);uniqueIndex" json:"-"` // 鍞竴绱㈠紩
	Password  string         `gorm:"type:varchar(255)" json:"-"`            // 涓嶈繑鍥炵粰鍓嶇
	CreatedAt time.Time      `json:"created_at"`                            // 鑷姩鏃堕棿鎴?
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"` // 鏀寔杞垹闄?
}
