FROM python:3.11-slim

WORKDIR /app

# Install git for cloning repositories
RUN apt-get update && apt-get install -y git

# Install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy the Flask application
COPY app.py scribe_core.py ./

# Copy the frontend assets
COPY capacitor/src ./capacitor/src/

EXPOSE 8080

CMD ["gunicorn", "--bind", "0.0.0.0:8080", "app:app"]
