package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	_ "github.com/go-sql-driver/mysql"
)

type Report struct {
	ID        int    `json:"id"`
	Armada    string `json:"armada"`
	Lokasi    string `json:"lokasi"`
	Deskripsi string `json:"deskripsi"`
	FotoURL   string `json:"foto_url"`
	Status    string `json:"status"`
}

var db *sql.DB

func main() {
	// Koneksi RDS
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbName := os.Getenv("DB_NAME")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s", dbUser, dbPass, dbHost, dbName)
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Println("Gagal koneksi DB:", err)
	} else {
		// Buat tabel jika belum ada
		createTable := `CREATE TABLE IF NOT EXISTS translapor (
			id INT AUTO_INCREMENT PRIMARY KEY,
			armada VARCHAR(255),
			lokasi VARCHAR(255),
			deskripsi TEXT,
			foto_key VARCHAR(255),
			status VARCHAR(50) DEFAULT 'Menunggu Verifikasi'
		);`
		db.Exec(createTable)
	}

	// Routing
	http.HandleFunc("/api/reports", handleReports)
	http.HandleFunc("/api/admin/update", handleAdminUpdate)

	// Frontend Pages
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html") // Halaman Publik
	})
	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "admin.html") // Halaman Admin
	})

	fmt.Println("TransLapor Server berjalan di port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleReports(w http.ResponseWriter, r *http.Request) {
	cdnDomain := os.Getenv("CDN_DOMAIN") // pakai CloudFront

	if r.Method == "POST" {
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			http.Error(w, "Gagal membaca form", http.StatusBadRequest)
			return
		}

		armada := r.FormValue("armada")
		lokasi := r.FormValue("lokasi")
		deskripsi := r.FormValue("deskripsi")
		file, header, err := r.FormFile("foto")
		if err != nil {
			http.Error(w, "Foto wajib diupload!", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Upload ke S3
		awsRegion := os.Getenv("AWS_REGION")
		bucketName := os.Getenv("S3_BUCKET")
		creds := credentials.NewStaticCredentials(os.Getenv("AWS_ACCESS_KEY"), os.Getenv("AWS_SECRET_KEY"), "")
		sess, _ := session.NewSession(&aws.Config{Region: aws.String(awsRegion), Credentials: creds})

		uploader := s3manager.NewUploader(sess)
		fileKey := "laporan/" + header.Filename
		_, err = uploader.Upload(&s3manager.UploadInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(fileKey),
			Body:   file,
			ACL:    aws.String("public-read"),
		})
		if err != nil {
			http.Error(w, "Gagal upload S3: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Simpan nama file (key) ke Database
		if db != nil {
			db.Exec("INSERT INTO translapor (armada, lokasi, deskripsi, foto_key) VALUES (?, ?, ?, ?)", armada, lokasi, deskripsi, fileKey)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"message": "Sukses"})
		return
	}

	if r.Method == "GET" {
		rows, _ := db.Query("SELECT id, armada, lokasi, deskripsi, foto_key, status FROM translapor ORDER BY id DESC")
		defer rows.Close()

		var reports []Report
		for rows.Next() {
			var r Report
			var fotoKey string
			rows.Scan(&r.ID, &r.Armada, &r.Lokasi, &r.Deskripsi, &fotoKey, &r.Status)

			// GABUNGKAN CDN CLOUDFRONT DENGAN FOTO KEY
			r.FotoURL = fmt.Sprintf("https://%s/%s", cdnDomain, fotoKey)
			reports = append(reports, r)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(reports)
	}
}

// Fitur Admin (Update Status)
func handleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		id := r.FormValue("id")
		status := r.FormValue("status")
		pin := r.FormValue("pin")

		// PIN Sederhana untuk keamanan
		if pin != "dinas123" {
			http.Error(w, "PIN Admin Salah!", http.StatusUnauthorized)
			return
		}

		if db != nil {
			db.Exec("UPDATE translapor SET status = ? WHERE id = ?", status, id)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "Status Diperbarui"})
	}
}
