# Amazon SQS
We use amazon `Simple Queue Service` for communicating between different hosts with asymmetric encryption (Public and Private key)

# Job Listener
`worker.events`: Email Events e.g. Imap Sync (FIFO grouped by EmailID)<br>
`worker.logs`: Logs

# Workers
`w<id>`: Send an event to the worker e.g. Email Add or Email Send
