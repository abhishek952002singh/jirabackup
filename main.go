package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"

	"github.com/andygrunwald/go-jira"
	_ "github.com/lib/pq"
)

var tmpl = template.Must(template.ParseFiles("index.html"))

// Database connection settings
const (
	host     = "localhost"
	port     = 5432
	user     = "postgres"
	password = "12345678" // replace with your actual password
	dbname   = "jira_backup"
)

var db *sql.DB
var syncStatus bool // Indicates if sync is working
var logs []string   // Stores log messages

// Initialize DB connection
func initDB() {
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	var err error
	db, err = sql.Open("postgres", psqlInfo)
	if err != nil {
		log.Fatal(err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Successfully connected to the database!")
}

func loginPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		tmpl.Execute(w, nil)
	} else {
		r.ParseForm()
		username := r.FormValue("username")
		password := r.FormValue("password")

		if username == "admin" && password == "admin" {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		} else {
			fmt.Fprintf(w, "Invalid credentials")
		}
	}
}

func dashboard(w http.ResponseWriter, r *http.Request) {
	dashboardTemplate := `
    <html>
    <body>
    <h2>Welcome to Jira Backup/Restore System</h2>
	    <p>This is a test to see if changes are reflecting---5</p>

    <form method="POST" action="/backup">
        <button type="submit">manual Backup Jira Projects</button>
    </form>
    <br/>
    <form method="POST" action="/restore">
        <button type="submit">Restore all Jira Projects</button>
    </form>
    
    <h3>Log Section</h3>
    <div id="logSection">
        <p id="syncStatus">Checking sync status...</p>
        <pre id="logMessages"></pre>
    </div>

    <script>
        // Fetch logs and sync status from the server
function fetchLogs() {
    fetch('/sync-status')
        .then(response => response.json())
        .then(data => {
            document.getElementById("syncStatus").textContent = data.syncStatus ? "Sync is working" : "Sync is not working";
            
            // Check if logs exist and are an array before using them
            if (Array.isArray(data.logs)) {
                document.getElementById("logMessages").textContent = data.logs.join("\n");
            } else {
                document.getElementById("logMessages").textContent = "No logs available";
            }
        })
        .catch(err => {
            document.getElementById("syncStatus").textContent = "Failed to get sync status";
            document.getElementById("logMessages").textContent = err.toString();
        });
}


        // Fetch logs every few seconds
        setInterval(fetchLogs, 5000); // Poll every 5 seconds
        fetchLogs(); // Initial load
    </script>

    </body>
    </html>
    `
	fmt.Fprint(w, dashboardTemplate)
}

// Backup Jira projects to the database
func backupProjects(w http.ResponseWriter, r *http.Request) {
	jiraClient, err := jira.NewClient(nil, "https://abhi952002singh.atlassian.net") // Replace with your Jira URL
	if err != nil {
		fmt.Fprintf(w, "Failed to create Jira client: %v", err)
		return
	}

	// Use your authentication token (replace with actual values)
	jiraClient.Authentication.SetBasicAuth("Abhi952002singh@gmail.com", "YOUR_API_TOKEN")

	// Fetch Jira projects
	projects, _, err := jiraClient.Project.GetList()
	if err != nil {
		fmt.Fprintf(w, "Error fetching Jira projects: %v", err)
		return
	}

	// Insert projects into PostgreSQL
	for _, project := range *projects {
		_, err := db.Exec(`INSERT INTO jira_projects (project_key, project_name, project_id) VALUES ($1, $2, $3)`,
			project.Key, project.Name, project.ID)
		if err != nil {
			fmt.Fprintf(w, "Error inserting project %v into the database: %v", project.Name, err)
			return
		}
	}

	fmt.Fprint(w, "Backup completed successfully!")
}

func restoreProjects(w http.ResponseWriter, r *http.Request) {
	// Jira REST API endpoint for creating projects
	jiraAPIURL := "https://abhi952002singh.atlassian.net/rest/api/2/project"

	// Fetch all projects from PostgreSQL
	rows, err := db.Query("SELECT project_key, project_name FROM jira_projects")
	if err != nil {
		fmt.Fprintf(w, "Error fetching projects from database: %v", err)
		return
	}
	defer rows.Close()

	// Use your actual lead account ID
	leadAccountId := "712020:0aba11f1-c25e-4bdf-b6a3-4c10a107530b"

	// Loop through each row and create the project in Jira
	for rows.Next() {
		var projectKey, projectName string
		if err := rows.Scan(&projectKey, &projectName); err != nil {
			fmt.Fprintf(w, "Error reading row: %v", err)
			return
		}

		// Validate projectKey to ensure it's not empty or invalid
		if len(projectKey) == 0 {
			fmt.Fprintf(w, "Project key is missing for project: %s\n", projectName)
			continue
		}

		// Create the request payload
		payload := map[string]interface{}{
			"assigneeType":       "PROJECT_LEAD",
			"key":                projectKey,
			"leadAccountId":      leadAccountId,
			"name":               projectName,
			"projectTemplateKey": "com.atlassian.jira-core-project-templates:jira-core-simplified-process-control",
			"projectTypeKey":     "business",
		}

		// Convert payload to JSON
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			fmt.Fprintf(w, "Error marshaling payload: %v", err)
			continue
		}

		// Log the request payload for debugging
		fmt.Printf("Payload: %s\n", string(payloadBytes))

		// Create a new HTTP POST request to the Jira API
		req, err := http.NewRequest("POST", jiraAPIURL, bytes.NewBuffer(payloadBytes))
		if err != nil {
			fmt.Fprintf(w, "Error creating HTTP request: %v", err)
			continue
		}

		// Set headers
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth("Abhi952002singh@gmail.com", "YOUR_API_TOKEN")

		// Execute the HTTP request
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(w, "Error making API request to Jira: %v", err)
			continue
		}
		defer resp.Body.Close()

		// Read response body for detailed error information
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Response: %s\n", string(body)) // Log response for debugging

		if resp.StatusCode == http.StatusCreated {
			fmt.Fprintf(w, "Successfully restored project: %s\n", projectName)
		} else {
			fmt.Fprintf(w, "Failed to create project %s: Jira API returned status code %d, response: %s\n", projectName, resp.StatusCode, string(body))
		}
	}

	if err := rows.Err(); err != nil {
		fmt.Fprintf(w, "Error with database rows: %v", err)
		return
	}

	fmt.Fprintf(w, "All projects restored successfully!")
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	logs = append(logs, "Received webhook event")
	fmt.Println("Received webhook event")

	var webhookEvent map[string]interface{}
	err := json.NewDecoder(r.Body).Decode(&webhookEvent)
	if err != nil {
		logs = append(logs, fmt.Sprintf("Error parsing webhook event: %v", err))
		fmt.Printf("Error parsing webhook event: %v\n", err)
		syncStatus = false
		http.Error(w, "Invalid webhook payload", http.StatusBadRequest)
		return
	}

	logs = append(logs, "Webhook event parsed successfully")
	fmt.Println("Webhook event parsed successfully")

	project, ok := webhookEvent["project"].(map[string]interface{})
	if !ok {
		logs = append(logs, "Invalid project data in webhook event")
		fmt.Println("Invalid project data in webhook event")
		syncStatus = false
		http.Error(w, "Invalid project data", http.StatusBadRequest)
		return
	}

	logs = append(logs, "Valid project data received from webhook")
	fmt.Println("Valid project data received from webhook")

	// Convert fields from interface{} to string properly
	projectKey, ok := project["key"].(string)
	if !ok {
		projectKeyFloat, ok := project["key"].(float64)
		if ok {
			projectKey = fmt.Sprintf("%.0f", projectKeyFloat)
		} else {
			logs = append(logs, "Invalid type for project key")
			syncStatus = false
			http.Error(w, "Invalid project key", http.StatusBadRequest)
			return
		}
	}

	projectName, ok := project["name"].(string)
	if !ok {
		logs = append(logs, "Invalid type for project name")
		syncStatus = false
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	projectId, ok := project["id"].(string)
	if !ok {
		projectIdFloat, ok := project["id"].(float64)
		if ok {
			projectId = fmt.Sprintf("%.0f", projectIdFloat)
		} else {
			logs = append(logs, "Invalid type for project ID")
			syncStatus = false
			http.Error(w, "Invalid project ID", http.StatusBadRequest)
			return
		}
	}

	// Log the data to be inserted
	logs = append(logs, fmt.Sprintf("Inserting project into DB: Key=%s, Name=%s, ID=%s", projectKey, projectName, projectId))
	fmt.Printf("Inserting project into DB: Key=%s, Name=%s, ID=%s\n", projectKey, projectName, projectId)

	// Insert the new project into the database
	_, err = db.Exec(`INSERT INTO jira_projects (project_key, project_name, project_id) VALUES ($1, $2, $3)`,
		projectKey, projectName, projectId)

	if err != nil {
		logs = append(logs, fmt.Sprintf("Error syncing project %s: %v", projectName, err))
		fmt.Printf("Error syncing project %s: %v\n", projectName, err)
		syncStatus = false
		fmt.Fprintf(w, "Error syncing project: %v", err)
		return
	}

	logs = append(logs, fmt.Sprintf("Successfully synced project: %s", projectName))
	fmt.Printf("Successfully synced project: %s\n", projectName)
	syncStatus = true

	fmt.Fprint(w, "Webhook received and processed successfully")
}

// Endpoint to fetch logs and sync status
func syncStatusHandler(w http.ResponseWriter, r *http.Request) {
	// Ensure that logs is not null
	if logs == nil {
		logs = []string{}
	}
	status := map[string]interface{}{
		"syncStatus": syncStatus,
		"logs":       logs,
	}
	json.NewEncoder(w).Encode(status)
}

func main() {
	// Initialize the database connection
	initDB()

	http.HandleFunc("/", loginPage)
	http.HandleFunc("/dashboard", dashboard)
	http.HandleFunc("/backup", backupProjects)
	http.HandleFunc("/restore", restoreProjects)
	http.HandleFunc("/webhook", webhookHandler)
	http.HandleFunc("/sync-status", syncStatusHandler) // Endpoint for fetching sync status and logs

	fmt.Println("Server started at :8080")
	http.ListenAndServe(":8080", nil)
}
