# Delivery Templates Guide

[中文](发货模板.zh-CN) | English

## Create a template

1. Open Delivery Templates and click New Delivery Template.
2. Enter a name and decide whether the template is enabled.
3. Enter one or more messages. They are sent separately in page order.
4. Click Add Message for additional messages. Unneeded messages can be removed, but a template must keep at least one message.
5. Click Save Template.

## Available variables

Template messages can use these variables:

- `{{order_id}}`: order ID
- `{{buyer_id}}`: buyer ID
- `{{buyer_nickname}}`: buyer nickname
- `{{card_name}}`: name of the card inventory bound to the rule
- `{{delivery_date}}`: delivery date, for example `2026-01-02`
- `{{delivery_time}}`: delivery date and time, for example `2026-01-02 15:04`
- `{{cards.<name>}}`: card content bound to the rule, for example `{{cards.main}}`
- `{{custom.<name>}}`: custom text entered in the automation rule

Variable names must not contain spaces or Chinese characters, and all braces must be present. The system validates variable spelling when saving.

`{{delivery_date}}` and `{{delivery_time}}` are rendered in Beijing time (UTC+8) from the moment the message is sent, not the moment the order was placed. Every message of one template action shares the same moment.

## Use a template in automation

1. Save the template, then open Automation.
2. Select it in a Paid Delivery or Review Gift rule.
3. Bind card inventory to every `cards` variable used by the template.
4. Enter text for every `custom` variable required by the template.
5. Save and enable the rule, then use a test order to verify message order and substitution.

Delivery templates are only used for paid delivery and review gifts. They are not used for price changes or review reminders. A template referenced by a rule cannot be deleted; replace it in those rules first.
