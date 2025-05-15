# repo.md - GitHub Repository Scribe

`repo.md` is a web application that takes a public GitHub repository URL, clones it, and generates a Markdown representation of its file structure and selected content. This allows for a quick overview or documentation of a repository's contents.

## ✨ Features

*   **Markdown Generation**: Creates a structured Markdown document from a GitHub repository.
*   **Modern UI**: A clean, GitHub-inspired user interface for a pleasant experience.
*   **Light/Dark Mode**: Adapts to your system's preferred color scheme.
*   **Copy to Clipboard**: Easily copy the generated Markdown.
*   **Download .md File**: Download the complete Markdown output as a `.md` file.
*   **Responsive Design**: Usable across different screen sizes.

## 🛠️ Tech Stack

*   **Backend**: Python 3 with Flask
*   **Frontend**: HTML, CSS (with CSS Variables for theming), JavaScript
*   **Core Logic**: Custom Python scripts (`scribe_core.py`) for repository parsing.
*   **Containerization**: Docker & Docker Compose

## 📁 Project Structure

```
repo.md/
├── capacitor/
│   └── src/
│       ├── index.html       # Main HTML file for the frontend
│       └── styles.css       # CSS styles for the application
├── web/
│   └── Dockerfile         # Dockerfile for building the web application (deprecated, see root Dockerfile)
├── app.py                 # Flask application backend
├── scribe_core.py         # Core Python logic for Markdown generation
├── Dockerfile             # Main Dockerfile for the application
├── docker-compose.yml     # Docker Compose configuration for easy setup
├── requirements.txt       # Python dependencies
└── README.md              # This file
```
*(Note: The `capacitor/` directory houses the frontend assets, but the project is currently run as a standard web application, not a full Capacitor mobile app.)*

## 🚀 Getting Started

### Prerequisites

*   Python 3.8+
*   Docker Desktop (or Docker Engine + Docker Compose)
*   Git (for cloning the repository and for the application to clone other repos)

### Installation & Running

1.  **Clone the repository:**
    ```bash
    git clone <your-repository-url> repo.md
    cd repo.md
    ```

2.  **Build and run with Docker Compose:**
    This is the recommended way to run the application. It handles building the Docker image and running the container.
    ```bash
    docker-compose up --build
    ```
    The application will typically be available at `http://localhost:5000` (or the port specified in `docker-compose.yml`).

3.  **Access the application:**
    Open your web browser and navigate to `http://localhost:5000`.

## 🤔 How It Works

1.  The user enters a GitHub repository URL into the frontend.
2.  The frontend sends this URL to the Flask backend (`/generate_markdown` endpoint).
3.  The Flask app (`app.py`) receives the request.
4.  The `scribe_core.py` module is invoked, which:
    *   Clones the specified GitHub repository into a temporary directory.
    *   Traverses the repository's file structure (respecting `.gitignore` if present, though current implementation might need verification on this specific detail).
    *   Concatenates file contents into a single Markdown string, formatted with headers for file paths.
5.  The generated Markdown content is sent back to the frontend as a JSON response.
6.  The frontend displays the Markdown, allowing the user to copy or download it.

## 🔮 Future Enhancements

*   Improved handling of large repositories or files.
*   Option to exclude specific files/directories or include only certain types.
*   More sophisticated Markdown formatting options.
*   User authentication and history of scribed repositories.
*   Deployment guides for platforms like Fly.io or Heroku.

## 📄 License

This project is currently under development and a formal license is yet to be defined. For now, assume it's for personal and educational use.

