The **Skupper Network Observer** is a monitoring and observability component designed to provide real-time visibility into your application network traffic. 
It acts as a bridge between the Skupper network layer and your monitoring stack, allowing you to visualize how data flows across different clusters and namespaces.

### What it Does

- **Traffic Introspection:** It connects to the local Skupper router to collect detailed metrics on requests, responses, and connection latencies.
- **Metrics Aggregation:** It includes a bundled, pre-configured Prometheus instance that scrapes and stores these networking metrics locally within the pod.
- **Secure Data Access:** It provides a REST API and UI backend that is protected by an integrated OpenShift OAuth proxy, ensuring that only authorized users can view network performance data.
- **Identity Management:** It automatically manages the mTLS certificates required to securely communicate with the Skupper router via the `Certificate` custom resource.

### Core Components

To understand how to configure the Observer, it is helpful to view it as a single pod containing three functional units:

1. **The Observer Engine:** (Container: `network-observer`) Processes raw Skupper protocol data into readable metrics.
2. **The Time-Series Database:** (Container: `prometheus`) Stores the metrics for historical analysis and graphing.
3. **The Security Gatekeeper:** (Container: `proxy`) Handles SSL/TLS termination and user authentication through OpenShift.