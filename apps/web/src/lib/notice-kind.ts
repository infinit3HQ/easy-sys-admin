/** A short label for a security notice kind. Unknown kinds stay as stored. */
export function noticeKind(kind: string) {
  switch (kind) {
    case 'vuln':
      return 'Known exploited'
    case 'access':
      return 'Approved access changed'
    case 'host_key':
      return 'Host key changed'
    case 'new_key':
      return 'New SSH key'
    case 'new_port':
      return 'New socket'
    default:
      return kind
  }
}
