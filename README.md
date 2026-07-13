# ImagePipeline

ImagePipeline is a serverless image processing, moderation, and semantic visual search pipeline built with AWS services, Go, and Pulumi. It automatically processes uploaded images, moderates content for safety, extracts descriptive labels, generates high-dimensional vector embeddings, and enables real-time semantic visual search via an API Gateway-backed web frontend queryable with natural language.

## System Architecture

### Image Processing Flow

```text
[ User ]        [ AWS S3 ]       [ AWS Lambda ]     [ AWS Rekognition ]    [ AWS Bedrock ]     [ OpenSearch ]
   │                 │                  │                    │                    │                   │
   │─── 1. Upload ──>│                  │                    │                    │                   │
   │                 │─── 2. Trigger ──>│                  (Detect)               │                   │
   │                 │    (s3:Created)  │                    │                    │                   │
   │                 │                  │─── 3. Moderate ───>│                    │                   │
   │                 │                  │<─── 4. Results ────│                    │                   │
   │                 │                  │                    │                    │                   │
   │                 │                  ├─── [ Explicit Content? ]                │                   │
   │                 │                  │    ├── Yes: Delete from S3              │                   │
   │                 │<─── 5. Delete ───│    └── No: Continue                     │                   │
   │                 │                  │                    │                    │                   │
   │                 │                  │─── 6. Label ──────>│                    │                   │
   │                 │                  │<─── 7. Metadata ───│                    │                   │
   │                 │                  │                                         │                   │
   │                 │                  │─── 8. Get Embedding (Titan) ───────────>│                   │
   │                 │                  │<─── 9. Vector (1024-dim) ───────────────│                   │
   │                 │                  │                                         │                   │
   │                 │                  │─── 10. Index Metadata & Vector ────────────────────────────>│
   │                 │                  │                                                             │
```

### Infrastructure Components

```text
                         +-------------------+
                         |   User / Browser  |
                         +----+---------+----+
                              |         |
                  (uploads)   |         | (GET /search)
                              v         v
                     +--------+---+   +-+-----------------+
                     |   AWS S3   |   |  AWS API Gateway  |
                     |   Bucket   |   |   (searchApi)     |
                     +----+-------+   +-+-----------------+
                          |             |
         (s3:ObjectCreated|             | (proxy trigger)
                          v             v
                     +----+-------+   +-+-----------------+
                     | AWS Lambda |   | AWS Lambda (Go)   |
                     | (onCreate) |   | (searchLambda)    |
                     +----+---+---+   +-+----+-------+----+
                          |   |              |       |
      (analyze/moderate)  |   | (embeddings) |       | (embeddings)
                          v   v              v       |
           +--------------+-+ +--------------+-+     |
           | AWS Rekognition| |  AWS Bedrock   |     |
           +----------------+ +----------------+     | (k-NN search)
                                                     v
                                              +------+------------+
                                              |    OpenSearch     |
                                              |  (Docker on EC2)  |
                                              +-------------------+
```

## Infrastructure Details

The infrastructure is defined programmatically via Pulumi in Go and consists of the following components:

* **AWS S3 Bucket**: Serves as the raw upload destination. It allows public read access for displaying images in the web frontend and has event notifications configured to trigger the Lambda processor.
* **AWS Lambda Functions**:
  * **onCreateLambda**: Triggers on S3 uploads. Conducts content moderation (Rekognition), extracts labels, queries Bedrock for multimodal image embeddings, and indexes the document.
  * **searchLambda**: Triggers via API Gateway. Converts user text search queries into embeddings via Bedrock and issues high-performance similarity queries against OpenSearch.
* **Amazon API Gateway**: Sets up an HTTP API with a `/search` route proxying requests directly to `searchLambda` with full CORS support.
* **Amazon Bedrock (Titan Multimodal Embeddings)**: Generates 1024-dimensional vector embeddings of both text (queries) and images (uploads) inside the same vector space.
* **Amazon EC2 Instance**: A `t3.micro` instance running Amazon Linux 2023. It hosts a single-node OpenSearch deployment inside Docker, configured with custom CORS parameters and k-NN search index template settings.
* **AWS IAM Roles & Policies**: Grants least-privilege permissions, including `bedrock:InvokeModel` specifically for the Titan embedding foundation model ARN.

## Key Features

* **Real-Time Automated Processing**: Triggered instantly upon S3 upload.
* **Automated Content Moderation**: Integrates with AWS Rekognition to detect explicit material. Offending images are deleted immediately from the S3 bucket.
* **In-Memory Format Conversion**: Automatically detects and converts non-JPEG image uploads (like PNG) to JPEG in-memory to ensure AWS Rekognition compatibility.
* **Semantic Vector Search**: Uses the Amazon Bedrock Titan model (`amazon.titan-embed-image-v1`) to compute semantic vectors of the search query and the indexed images.
* **k-NN Vector Similarity Matching**: Queries the high-dimensional OpenSearch k-NN index using cosine similarity to return the most contextually relevant visual matches.
* **Dynamic Configuration**: The frontend (`index.html`) automatically updates with the correct S3 and API Gateway endpoints using the `configure.sh` automation script, while also supporting runtime overrides via browser localStorage.

## Directory Structure

* `/lambda/on-create`: Source code for the S3-triggered metadata extraction and indexing Lambda.
* `/lambda/search`: Source code for the API Gateway-triggered visual search query Lambda.
* `/opensearch`: Pulumi configuration for provisioning the EC2 host and setting up Docker with OpenSearch.
* `/uploads`: Pulumi configuration for the S3 bucket, event notification triggers, and IAM policies.
* `/search`: Pulumi configuration for API Gateway and the Search Lambda infrastructure.
* `/scripts`: Utility scripts, including configuration injection for the web UI.
* `/images`: Artifacts and screenshots for documentation.
* `main.go`: The entrypoint for Pulumi infrastructure deployment.
* `index.html`: The lightweight, client-side visual search interface.

## Getting Started

### Prerequisites

* Go 1.20 or later
* Pulumi CLI installed and authenticated
* AWS CLI installed and configured with appropriate credentials

### Deployment Steps

1. Deploy the infrastructure using Pulumi:
   ```bash
   pulumi up
   ```

2. Once the deployment finishes, update the web interface configuration with the live output values:
   ```bash
   ./scripts/configure.sh
   ```

3. Open `index.html` in a web browser to search for uploaded images.

## Media and Interface

### User Interface Screenshot
![Search Interface Main](images/image.png)

### Search Query Result
![Search Interface Results](images/image%20copy.png)