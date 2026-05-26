package main

import "github.com/skip2/go-qrcode"

const userLinkTelegramQRCodeSize = 512

func renderUserLinkQRCodePNG(link string) ([]byte, error) {
	return qrcode.Encode(link, qrcode.Medium, userLinkTelegramQRCodeSize)
}
