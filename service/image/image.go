package image

import (
	"ai-chat/common/image"
	"io"
	"log"
	"mime/multipart"
)

func RecognizeImage(file *multipart.FileHeader) (string, error) {

	modelPath := "/root/models/mobilenetv2/mobilenetv2-7.onnx"
	labelPath := "/root/imagenet_classes.txt"
	inputH, inputW := 224, 224

	recognizer, err := image.NewImageRecognizer(modelPath, labelPath, inputH, inputW)
	if err != nil {
		log.Println("NewImageRecognizer fail err is : ", err)
		return "", err
	}
	defer recognizer.Close()

	src, err := file.Open()
	if err != nil {
		log.Println("file open fail err is : ", err)
		return "", err
	}
	defer src.Close()

	buf, err := io.ReadAll(src)
	if err != nil {
		log.Println("io.ReadAll fail err is : ", err)
		return "", err
	}

	return recognizer.PredictFromBuffer(buf)
}
