package events

import (
	"bytes"
	"fmt"
	"time"
)

// FormatGetEventPropertiesResponse returns the SOAP envelope advertising supported event topics.
func FormatGetEventPropertiesResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsa="http://www.w3.org/2005/08/addressing"
               xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
               xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"
               xmlns:wstop="http://docs.oasis-open.org/wsn/t-1"
               xmlns:tns1="http://www.onvif.org/ver10/topics">
  <soap:Header>
    <wsa:Action>http://www.onvif.org/ver10/events/wsdl/EventPortType/GetEventPropertiesResponse</wsa:Action>
  </soap:Header>
  <soap:Body>
    <tev:GetEventPropertiesResponse>
      <tev:TopicNamespaceLocation>http://www.onvif.org/onvif/ver10/topics/topicns.xml</tev:TopicNamespaceLocation>
      <wsnt:FixedTopicSet>true</wsnt:FixedTopicSet>
      <wstop:TopicSet>
        <tns1:RuleEngine>
          <CellMotionDetector>
            <Motion wstop:topic="true"/>
          </CellMotionDetector>
          <PeopleDetector>
            <People wstop:topic="true"/>
          </PeopleDetector>
          <VehicleDetector>
            <Vehicle wstop:topic="true"/>
          </VehicleDetector>
        </tns1:RuleEngine>
      </wstop:TopicSet>
      <wsnt:TopicExpressionDialect>http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet</wsnt:TopicExpressionDialect>
      <wsnt:TopicExpressionDialect>http://docs.oasis-open.org/wsn/t-1/TopicExpression/Concrete</wsnt:TopicExpressionDialect>
      <tev:MessageContentFilterDialect>http://www.onvif.org/ver10/tev/messageContentFilter/ItemFilter</tev:MessageContentFilterDialect>
    </tev:GetEventPropertiesResponse>
  </soap:Body>
</soap:Envelope>`
}

// FormatCreatePullPointSubscriptionResponse returns the subscription endpoint reference.
func FormatCreatePullPointSubscriptionResponse(endpoint string, currentTime, terminationTime time.Time) string {
	curStr := currentTime.Format(time.RFC3339)
	termStr := terminationTime.Format(time.RFC3339)

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsa="http://www.w3.org/2005/08/addressing"
               xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
               xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
  <soap:Header>
    <wsa:Action>http://www.onvif.org/ver10/events/wsdl/EventPortType/CreatePullPointSubscriptionResponse</wsa:Action>
  </soap:Header>
  <soap:Body>
    <tev:CreatePullPointSubscriptionResponse>
      <tev:SubscriptionReference>
        <wsa:Address>%s</wsa:Address>
      </tev:SubscriptionReference>
      <wsnt:CurrentTime>%s</wsnt:CurrentTime>
      <wsnt:TerminationTime>%s</wsnt:TerminationTime>
    </tev:CreatePullPointSubscriptionResponse>
  </soap:Body>
</soap:Envelope>`, endpoint, curStr, termStr)
}

// FormatPullMessagesResponse builds the XML response containing queued events.
func FormatPullMessagesResponse(currentTime, terminationTime time.Time, evList []Event) string {
	curStr := currentTime.Format(time.RFC3339)
	termStr := terminationTime.Format(time.RFC3339)

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsa="http://www.w3.org/2005/08/addressing"
               xmlns:tev="http://www.onvif.org/ver10/events/wsdl"
               xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"
               xmlns:tt="http://www.onvif.org/ver10/schema">
  <soap:Header>
    <wsa:Action>http://www.onvif.org/ver10/events/wsdl/PullPointSubscription/PullMessagesResponse</wsa:Action>
  </soap:Header>
  <soap:Body>
    <tev:PullMessagesResponse>
      <tev:CurrentTime>`)
	buf.WriteString(curStr)
	buf.WriteString(`</tev:CurrentTime>
      <tev:TerminationTime>`)
	buf.WriteString(termStr)
	buf.WriteString(`</tev:TerminationTime>
`)

	for _, ev := range evList {
		buf.WriteString(`      <wsnt:NotificationMessage>
        <wsnt:Topic Dialect="http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet">`)
		buf.WriteString(ev.Topic)
		buf.WriteString(`</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="`)
		buf.WriteString(ev.UtcTime.Format(time.RFC3339))
		buf.WriteString(`" PropertyOperation="Changed">
            <tt:Source>
              <tt:SimpleItem Name="VideoSourceToken" Value="VideoSourceToken"/>
            </tt:Source>
            <tt:Data>
`)
		for k, v := range ev.SimpleItems {
			buf.WriteString(fmt.Sprintf(`              <tt:SimpleItem Name="%s" Value="%s"/>`+"\n", k, v))
		}
		if ev.HasBBox {
			// normalized coordinates: left, top, right, bottom
			left := ev.BBox[0]
			top := ev.BBox[1]
			right := left + ev.BBox[2]
			bottom := top + ev.BBox[3]
			buf.WriteString(fmt.Sprintf(`              <tt:ElementItem Name="BoundingBox">
                <tt:Box left="%.2f" top="%.2f" right="%.2f" bottom="%.2f"/>
              </tt:ElementItem>`+"\n", left, top, right, bottom))
		}
		buf.WriteString(`            </tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
`)
	}

	buf.WriteString(`    </tev:PullMessagesResponse>
  </soap:Body>
</soap:Envelope>`)

	return buf.String()
}

// FormatRenewResponse returns the SOAP envelope for subscription renewal.
func FormatRenewResponse(currentTime, terminationTime time.Time) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsa="http://www.w3.org/2005/08/addressing"
               xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
  <soap:Header>
    <wsa:Action>http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/RenewResponse</wsa:Action>
  </soap:Header>
  <soap:Body>
    <wsnt:RenewResponse>
      <wsnt:CurrentTime>%s</wsnt:CurrentTime>
      <wsnt:TerminationTime>%s</wsnt:TerminationTime>
    </wsnt:RenewResponse>
  </soap:Body>
</soap:Envelope>`, currentTime.Format(time.RFC3339), terminationTime.Format(time.RFC3339))
}

// FormatUnsubscribeResponse returns the SOAP envelope for unsubscription.
func FormatUnsubscribeResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"
               xmlns:wsa="http://www.w3.org/2005/08/addressing"
               xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2">
  <soap:Header>
    <wsa:Action>http://docs.oasis-open.org/wsn/bw-2/SubscriptionManager/UnsubscribeResponse</wsa:Action>
  </soap:Header>
  <soap:Body>
    <wsnt:UnsubscribeResponse/>
  </soap:Body>
</soap:Envelope>`
}
