package main

import (
	"embed"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/shirou/gopsutil/v4/mem"
	"net/http"
	"time"
)

const kbToMb = 1024 * 1024

// responseMessage, webSocket üzerinden göndereceğimiz mesajın yapısı.
type responseMessage struct {
	TotalMemory          string
	FreeMemory           string
	UsedMemory           string
	PercentageUsedMemory string
	Time                 string
}

var (
	// tüm websocket client'larını tutacağımız map.
	clients = make(map[*websocket.Conn]bool)

	// oluşturduğumuz goroutine'ler arası iletişimi sağlayacağımız responseMessage tipinde channel.
	broadcast = make(chan *responseMessage)

	// http bağlantımızı websocket'e çeviren gorilla kütüphanesi metodu ve config'leri
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

//go:embed web
var webAssets embed.FS

func main() {
	e := echo.New()

	// Web dosyalarını sunmak için middleware'i ayarlıyoruz.
	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Root:       "web",
		Filesystem: http.FS(webAssets),
	}))

	// WebSocket bağlantılarını yönetmek için handler tanımlıyoruz.
	e.GET("/wsUrl", handleConnections)

	// WebSocket mesajlarını yönetecek bir goroutine başlatıyoruz.
	go handleMessages()

	// Bellek istatistiklerini toplayacak bir goroutine başlatıyoruz.
	go collectStats()

	e.Logger.Fatal(e.Start(":3000"))
}

// handleConnections, yeni ve kapanan websocket bağlantılarını yönetir.
func handleConnections(c echo.Context) error {

	// http bağlantımızı,websocket bağlantısına dönüştürüyoruz.
	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		log.Fatalf("Failed to upgrade connection: %v", err)
		return err
	}

	defer ws.Close()

	// Yeni bağlantıyı clients map'ine ekliyoruz.
	clients[ws] = true

	for {
		// WebSocket mesajlarını okuyoruz. socket'in kapanması ve hata durumunda,
		// önceden clients map'ine eklediğimiz bağlantıyı temizliyoruz.
		_, _, err = ws.ReadMessage()
		if err != nil {
			log.Printf("error: %v", err)
			delete(clients, ws)
			break
		}
	}
	return nil
}

// handleMessages, broadcast kanalından gelen mesajları, tüm websocket bağlantılara gönderir.
func handleMessages() {
	for {
		msg := <-broadcast
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				log.Printf("error: %v", err)
				client.Close()
				delete(clients, client)
			}
		}
	}
}

// collectStats, sistem bellek bilgilerini toplayıp broadcast kanalına gönderir.
// insani olması açısından 1 saniye olarak ayarlandım.isterseniz kaldırabilirsiniz.
func collectStats() {
	for {
		v, err := mem.VirtualMemory()
		if err != nil {
			log.Printf("error: %v", err)
			continue
		}

		totalMemory := fmt.Sprintf("%d", v.Total/kbToMb)
		userMemory := fmt.Sprintf("%d", v.Used/kbToMb)
		freeMemory := fmt.Sprintf("%d", v.Free/kbToMb)
		percentUsage := fmt.Sprintf("%.2f", v.UsedPercent)
		timeNow := time.Now().Format("02-01-2006 15:04:05")

		broadcast <- &responseMessage{
			TotalMemory:          totalMemory,
			UsedMemory:           userMemory,
			FreeMemory:           freeMemory,
			PercentageUsedMemory: percentUsage,
			Time:                 timeNow,
		}

		time.Sleep(1 * time.Second)
	}
}
